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

package kubecfg

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"text/template"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/build/buildapi"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"gopkg.in/v1/yaml"
)

// ResourcePrinter is an interface that knows how to print API resources
type ResourcePrinter interface {
	// Print receives an arbitrary JSON body, formats it and prints it to a writer
	Print([]byte, io.Writer) error
	PrintObj(interface{}, io.Writer) error
}

// IdentityPrinter is an implementation of ResourcePrinter which simply copies the body out to the output stream
type IdentityPrinter struct{}

// Print is an implementation of ResourcePrinter.Print which simply writes the data to the Writer.
func (i *IdentityPrinter) Print(data []byte, w io.Writer) error {
	_, err := w.Write(data)
	return err
}

// PrintObj is an implementation of ResourcePrinter.PrintObj which simply writes the object to the Writer.
func (i *IdentityPrinter) PrintObj(obj interface{}, output io.Writer) error {
	data, err := api.Encode(obj)
	if err != nil {
		return err
	}
	return i.Print(data, output)
}

// YAMLPrinter is an implementation of ResourcePrinter which parsess JSON, and re-formats as YAML
type YAMLPrinter struct{}

// Print parses the data as JSON, re-formats as YAML and prints the YAML.
func (y *YAMLPrinter) Print(data []byte, w io.Writer) error {
	var obj interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	output, err := yaml.Marshal(obj)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(w, string(output))
	return err
}

// PrintObj prints the data as YAML.
func (y *YAMLPrinter) PrintObj(obj interface{}, w io.Writer) error {
	output, err := yaml.Marshal(obj)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(w, string(output))
	return err
}

// HumanReadablePrinter is an implementation of ResourcePrinter which attempts to provide more elegant output.
type HumanReadablePrinter struct{}

var podColumns = []string{"Name", "Image(s)", "Host", "Labels"}
var replicationControllerColumns = []string{"Name", "Image(s)", "Selector", "Replicas"}
var serviceColumns = []string{"Name", "Labels", "Selector", "Port"}
var minionColumns = []string{"Minion identifier"}
var statusColumns = []string{"Status"}
var buildColumns = []string{"ID", "Status", "Pod ID"}

func (h *HumanReadablePrinter) unknown(data []byte, w io.Writer) error {
	_, err := fmt.Fprintf(w, "Unknown object: %s", string(data))
	return err
}

func (h *HumanReadablePrinter) printHeader(columnNames []string, w io.Writer) error {
	if _, err := fmt.Fprintf(w, "%s\n", strings.Join(columnNames, "\t")); err != nil {
		return err
	}
	var lines []string
	for _ = range columnNames {
		lines = append(lines, "----------")
	}
	_, err := fmt.Fprintf(w, "%s\n", strings.Join(lines, "\t"))
	return err
}

func (h *HumanReadablePrinter) makeImageList(manifest api.ContainerManifest) string {
	var images []string
	for _, container := range manifest.Containers {
		images = append(images, container.Image)
	}
	return strings.Join(images, ",")
}

func (h *HumanReadablePrinter) printPod(pod *api.Pod, w io.Writer) error {
	_, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
		pod.ID, h.makeImageList(pod.DesiredState.Manifest), pod.CurrentState.Host+"/"+pod.CurrentState.HostIP, labels.Set(pod.Labels))
	return err
}

func (h *HumanReadablePrinter) printPodList(podList *api.PodList, w io.Writer) error {
	for _, pod := range podList.Items {
		if err := h.printPod(&pod, w); err != nil {
			return err
		}
	}
	return nil
}

func (h *HumanReadablePrinter) printBuild(build *buildapi.Build, w io.Writer) error {
	_, err := fmt.Fprintf(w, "%s\t%s\t%s\n", build.ID, build.Status, build.PodID)
	return err
}

func (h *HumanReadablePrinter) printBuildList(buildList *buildapi.BuildList, w io.Writer) error {
	for _, build := range buildList.Items {
		if err := h.printBuild(&build, w); err != nil {
			return err
		}
	}
	return nil
}

func (h *HumanReadablePrinter) printReplicationController(ctrl *api.ReplicationController, w io.Writer) error {
	_, err := fmt.Fprintf(w, "%s\t%s\t%s\t%d\n",
		ctrl.ID, h.makeImageList(ctrl.DesiredState.PodTemplate.DesiredState.Manifest), labels.Set(ctrl.DesiredState.ReplicaSelector), ctrl.DesiredState.Replicas)
	return err
}

func (h *HumanReadablePrinter) printReplicationControllerList(list *api.ReplicationControllerList, w io.Writer) error {
	for _, ctrl := range list.Items {
		if err := h.printReplicationController(&ctrl, w); err != nil {
			return err
		}
	}
	return nil
}

func (h *HumanReadablePrinter) printService(svc *api.Service, w io.Writer) error {
	_, err := fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", svc.ID, labels.Set(svc.Labels), labels.Set(svc.Selector), svc.Port)
	return err
}

func (h *HumanReadablePrinter) printServiceList(list *api.ServiceList, w io.Writer) error {
	for _, svc := range list.Items {
		if err := h.printService(&svc, w); err != nil {
			return err
		}
	}
	return nil
}

func (h *HumanReadablePrinter) printMinion(minion *api.Minion, w io.Writer) error {
	_, err := fmt.Fprintf(w, "%s\n", minion.ID)
	return err
}

func (h *HumanReadablePrinter) printMinionList(list *api.MinionList, w io.Writer) error {
	for _, minion := range list.Items {
		if err := h.printMinion(&minion, w); err != nil {
			return err
		}
	}
	return nil
}

func (h *HumanReadablePrinter) printStatus(status *api.Status, w io.Writer) error {
	err := h.printHeader(statusColumns, w)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%v\n", status.Status)
	return err
}

// Print parses the data as JSON, then prints the parsed data in a human-friendly format according to the type of the data.
func (h *HumanReadablePrinter) Print(data []byte, output io.Writer) error {
	var mapObj map[string]interface{}
	if err := json.Unmarshal([]byte(data), &mapObj); err != nil {
		return err
	}

	if _, contains := mapObj["kind"]; !contains {
		return fmt.Errorf("unexpected object with no 'kind' field: %s", data)
	}

	obj, err := api.Decode(data)
	if err != nil {
		return err
	}
	return h.PrintObj(obj, output)
}

// PrintObj prints the obj in a human-friendly format according to the type of the obj.
func (h *HumanReadablePrinter) PrintObj(obj interface{}, output io.Writer) error {
	w := tabwriter.NewWriter(output, 20, 5, 3, ' ', 0)
	defer w.Flush()
	switch o := obj.(type) {
	case *api.Pod:
		h.printHeader(podColumns, w)
		return h.printPod(o, w)
	case *api.PodList:
		h.printHeader(podColumns, w)
		return h.printPodList(o, w)
	case *api.ReplicationController:
		h.printHeader(replicationControllerColumns, w)
		return h.printReplicationController(o, w)
	case *api.ReplicationControllerList:
		h.printHeader(replicationControllerColumns, w)
		return h.printReplicationControllerList(o, w)
	case *api.Service:
		h.printHeader(serviceColumns, w)
		return h.printService(o, w)
	case *api.ServiceList:
		h.printHeader(serviceColumns, w)
		return h.printServiceList(o, w)
	case *api.Minion:
		h.printHeader(minionColumns, w)
		return h.printMinion(o, w)
	case *api.MinionList:
		h.printHeader(minionColumns, w)
		return h.printMinionList(o, w)
	case *api.Status:
		return h.printStatus(o, w)
	case *buildapi.Build:
		h.printHeader(buildColumns, w)
		return h.printBuild(o, w)
	case *buildapi.BuildList:
		h.printHeader(buildColumns, w)
		return h.printBuildList(o, w)
	default:
		_, err := fmt.Fprintf(w, "Error: unknown type %#v", obj)
		return err
	}
}

// TemplatePrinter is an implementation of ResourcePrinter which formats data with a Go Template.
type TemplatePrinter struct {
	Template *template.Template
}

// Print parses the data as JSON, and re-formats it with the Go Template.
func (t *TemplatePrinter) Print(data []byte, w io.Writer) error {
	obj, err := api.Decode(data)
	if err != nil {
		return err
	}
	return t.PrintObj(obj, w)
}

// PrintObj formats the obj with the Go Template.
func (t *TemplatePrinter) PrintObj(obj interface{}, w io.Writer) error {
	return t.Template.Execute(w, obj)
}
