/*
Copyright 2014 Google Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package apiserver

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path"
	"runtime/debug"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/healthz"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/httplog"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/version"
	"github.com/golang/glog"
)

// Codec defines methods for serializing and deserializing API
// objects
type Codec interface {
	Encode(obj interface{}) (data []byte, err error)
	Decode(data []byte) (interface{}, error)
	DecodeInto(data []byte, obj interface{}) error
}

// APIServer is an HTTPHandler that delegates to RESTStorage objects.
// It handles URLs of the form:
// ${prefix}/${storage_key}[/${object_name}]
// Where 'prefix' is an arbitrary string, and 'storage_key' points to a RESTStorage object stored in storage.
//
// TODO: consider migrating this to go-restful which is a more full-featured version of the same thing.
type APIServer struct {
	storage     map[string]RESTStorage
	codec       Codec
	ops         *Operations
	asyncOpWait time.Duration
	handler     http.Handler
}

// New creates a new APIServer object. 'storage' contains a map of handlers. 'codec'
// is an interface for decoding to and from JSON. 'prefix' is the hosting path prefix.
//
// The codec will be used to decode the request body into an object pointer returned by
// RESTStorage.New().  The Create() and Update() methods should cast their argument to
// the type returned by New().
// TODO: add multitype codec serialization
func New(storage map[string]RESTStorage, codec Codec, prefix string) *APIServer {
	s := &APIServer{
		storage: storage,
		codec:   codec,
		ops:     NewOperations(),
		// Delay just long enough to handle most simple write operations
		asyncOpWait: time.Millisecond * 25,
	}

	mux := http.NewServeMux()

	prefix = strings.TrimRight(prefix, "/")

	// Primary API handlers
	restPrefix := prefix + "/"
	mux.Handle(restPrefix, http.StripPrefix(restPrefix, http.HandlerFunc(s.handleREST)))

	// Watch API handlers
	watchPrefix := path.Join(prefix, "watch") + "/"
	mux.Handle(watchPrefix, http.StripPrefix(watchPrefix, &WatchHandler{storage, codec}))

	// Support services for the apiserver
	logsPrefix := "/logs/"
	mux.Handle(logsPrefix, http.StripPrefix(logsPrefix, http.FileServer(http.Dir("/var/log/"))))
	healthz.InstallHandler(mux)
	mux.HandleFunc("/version", handleVersion)
	mux.HandleFunc("/", handleIndex)

	// Handle both operations and operations/* with the same handler
	handler := &OperationHandler{s.ops, s.codec}
	operationPrefix := path.Join(prefix, "operations")
	mux.Handle(operationPrefix, http.StripPrefix(operationPrefix, handler))
	operationsPrefix := operationPrefix + "/"
	mux.Handle(operationsPrefix, http.StripPrefix(operationsPrefix, handler))

	// Proxy minion requests
	mux.Handle("/proxy/minion/", http.StripPrefix("/proxy/minion", http.HandlerFunc(handleProxyMinion)))

	s.handler = mux

	return s
}

// ServeHTTP implements the standard net/http interface.
func (s *APIServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	defer func() {
		if x := recover(); x != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "apis panic. Look in log for details.")
			glog.Infof("APIServer panic'd on %v %v: %#v\n%s\n", req.Method, req.RequestURI, x, debug.Stack())
		}
	}()
	defer httplog.MakeLogged(req, &w).StacktraceWhen(
		httplog.StatusIsNot(
			http.StatusOK,
			http.StatusAccepted,
			http.StatusConflict,
			http.StatusNotFound,
		),
	).Log()

	// Dispatch to the internal handler
	s.handler.ServeHTTP(w, req)
}

// handleREST handles requests to all our RESTStorage objects.
func (s *APIServer) handleREST(w http.ResponseWriter, req *http.Request) {
	parts := splitPath(req.URL.Path)
	if len(parts) < 1 {
		notFound(w, req)
		return
	}
	storage := s.storage[parts[0]]
	if storage == nil {
		httplog.LogOf(w).Addf("'%v' has no storage object", parts[0])
		notFound(w, req)
		return
	}

	s.handleRESTStorage(parts, req, w, storage)
}

// handleRESTStorage is the main dispatcher for a storage object.  It switches on the HTTP method, and then
// on path length, according to the following table:
//   Method     Path          Action
//   GET        /foo          list
//   GET        /foo/bar      get 'bar'
//   POST       /foo          create
//   PUT        /foo/bar      update 'bar'
//   DELETE     /foo/bar      delete 'bar'
// Returns 404 if the method/pattern doesn't match one of these entries
// The s accepts several query parameters:
//    sync=[false|true] Synchronous request (only applies to create, update, delete operations)
//    timeout=<duration> Timeout for synchronous requests, only applies if sync=true
//    labels=<label-selector> Used for filtering list operations
func (s *APIServer) handleRESTStorage(parts []string, req *http.Request, w http.ResponseWriter, storage RESTStorage) {
	sync := req.URL.Query().Get("sync") == "true"
	timeout := parseTimeout(req.URL.Query().Get("timeout"))
	switch req.Method {
	case "GET":
		switch len(parts) {
		case 1:
			selector, err := labels.ParseSelector(req.URL.Query().Get("labels"))
			if err != nil {
				errorJSON(err, s.codec, w)
				return
			}
			list, err := storage.List(selector)
			if err != nil {
				errorJSON(err, s.codec, w)
				return
			}
			writeJSON(http.StatusOK, s.codec, list, w)
		case 2:
			item, err := storage.Get(parts[1])
			if err != nil {
				errorJSON(err, s.codec, w)
				return
			}
			writeJSON(http.StatusOK, s.codec, item, w)
		default:
			notFound(w, req)
		}

	case "POST":
		if len(parts) != 1 {
			notFound(w, req)
			return
		}
		body, err := readBody(req)
		if err != nil {
			errorJSON(err, s.codec, w)
			return
		}
		obj := storage.New()
		err = s.codec.DecodeInto(body, obj)
		if err != nil {
			errorJSON(err, s.codec, w)
			return
		}
		out, err := storage.Create(obj)
		if err != nil {
			errorJSON(err, s.codec, w)
			return
		}
		op := s.createOperation(out, sync, timeout)
		s.finishReq(op, w)

	case "DELETE":
		if len(parts) != 2 {
			notFound(w, req)
			return
		}
		out, err := storage.Delete(parts[1])
		if err != nil {
			errorJSON(err, s.codec, w)
			return
		}
		op := s.createOperation(out, sync, timeout)
		s.finishReq(op, w)

	case "PUT":
		if len(parts) != 2 {
			notFound(w, req)
			return
		}
		body, err := readBody(req)
		if err != nil {
			errorJSON(err, s.codec, w)
			return
		}
		obj := storage.New()
		err = s.codec.DecodeInto(body, obj)
		if err != nil {
			errorJSON(err, s.codec, w)
			return
		}
		out, err := storage.Update(obj)
		if err != nil {
			errorJSON(err, s.codec, w)
			return
		}
		op := s.createOperation(out, sync, timeout)
		s.finishReq(op, w)

	default:
		notFound(w, req)
	}
}

// handleVersionReq writes the server's version information.
func handleVersion(w http.ResponseWriter, req *http.Request) {
	writeRawJSON(http.StatusOK, version.Get(), w)
}

// createOperation creates an operation to process a channel response
func (s *APIServer) createOperation(out <-chan interface{}, sync bool, timeout time.Duration) *Operation {
	op := s.ops.NewOperation(out)
	if sync {
		op.WaitFor(timeout)
	} else if s.asyncOpWait != 0 {
		op.WaitFor(s.asyncOpWait)
	}
	return op
}

// finishReq finishes up a request, waiting until the operation finishes or, after a timeout, creating an
// Operation to receive the result and returning its ID down the writer.
func (s *APIServer) finishReq(op *Operation, w http.ResponseWriter) {
	obj, complete := op.StatusOrResult()
	if complete {
		status := http.StatusOK
		switch stat := obj.(type) {
		case api.Status:
			httplog.LogOf(w).Addf("programmer error: use *api.Status as a result, not api.Status.")
			if stat.Code != 0 {
				status = stat.Code
			}
		case *api.Status:
			if stat.Code != 0 {
				status = stat.Code
			}
		}
		writeJSON(status, s.codec, obj, w)
	} else {
		writeJSON(http.StatusAccepted, s.codec, obj, w)
	}
}

// writeJSON renders an object as JSON to the response
func writeJSON(statusCode int, codec Codec, object interface{}, w http.ResponseWriter) {
	output, err := codec.Encode(object)
	if err != nil {
		errorJSON(err, codec, w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(output)
}

// errorJSON renders an error to the response
func errorJSON(err error, codec Codec, w http.ResponseWriter) {
	status := errToAPIStatus(err)
	writeJSON(status.Code, codec, status, w)
}

// writeRawJSON writes a non-API object in JSON.
func writeRawJSON(statusCode int, object interface{}, w http.ResponseWriter) {
	output, err := json.Marshal(object)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(output)
}

func parseTimeout(str string) time.Duration {
	if str != "" {
		timeout, err := time.ParseDuration(str)
		if err == nil {
			return timeout
		}
		glog.Errorf("Failed to parse: %#v '%s'", err, str)
	}
	return 30 * time.Second
}

func readBody(req *http.Request) ([]byte, error) {
	defer req.Body.Close()
	return ioutil.ReadAll(req.Body)
}

// splitPath returns the segments for a URL path
func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return []string{}
	}
	return strings.Split(path, "/")
}
