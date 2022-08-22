package patterns

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/gofrs/uuid"
	"github.com/layer5io/meshery/meshes"
	"github.com/layer5io/meshery/models/pattern/patterns/application"
	"github.com/layer5io/meshery/models/pattern/patterns/k8s"
	"github.com/layer5io/meshkit/models/oam/core/v1alpha1"
	"github.com/layer5io/meshkit/utils/events"
	"github.com/layer5io/meshkit/utils/kubernetes"
)

func ProcessOAM(kconfigs []string, oamComps []string, oamConfig string, isDel bool, eb *events.EventBuffer) (string, error) {
	var comps []v1alpha1.Component
	var config v1alpha1.Configuration

	for _, oamComp := range oamComps {
		var comp v1alpha1.Component
		if err := json.Unmarshal([]byte(oamComp), &comp); err != nil {
			return "", err
		}

		comps = append(comps, comp)
	}

	if err := json.Unmarshal([]byte(oamConfig), &config); err != nil {
		return "", err
	}

	var msgs []string
	var errs []error
	var kclis []*kubernetes.Client
	for _, config := range kconfigs {
		cli, err := kubernetes.New([]byte(config))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		kclis = append(kclis, cli)
	}
	var wg sync.WaitGroup
	for _, kcli := range kclis {
		wg.Add(1)
		go func(kcli *kubernetes.Client) {
			defer wg.Done()
			for _, comp := range comps {
				id, _ := uuid.NewV4()
				var req meshes.EventsResponse
				if comp.Spec.Type == "Application" {
					if err := application.Deploy(kcli, comp, config, isDel); err != nil {
						req = meshes.EventsResponse{
							EventType:   meshes.EventType_ERROR,
							Summary:     "Error deploying application: " + comp.Name,
							Details:     err.Error(),
							OperationId: id.String(),
						}
						errs = append(errs, err)
						continue
					}
					if !isDel {
						req = meshes.EventsResponse{
							EventType:   meshes.EventType_INFO,
							Summary:     "Successfully deployed application " + comp.Name,
							OperationId: id.String(),
						}
						msgs = append(msgs, "successfully deployed application "+comp.Name)
					} else {
						req = meshes.EventsResponse{
							EventType:   meshes.EventType_INFO,
							Summary:     "Successfully deleted application " + comp.Name,
							OperationId: id.String(),
						}

						msgs = append(msgs, "successfully deleted application "+comp.Name)
					}
					eb.Enqueue(&req)
					continue
				}

				// Handle all of the k8s component here
				if strings.HasSuffix(strings.ToLower(comp.Spec.Type), ".k8s") {
					if err := k8s.Deploy(kcli, comp, config, isDel); err != nil {
						errs = append(errs, err)
						req = meshes.EventsResponse{
							EventType:   meshes.EventType_ERROR,
							Summary:     "Error deploying k8s component: " + comp.Name,
							Details:     err.Error(),
							OperationId: id.String(),
						}
						continue
					}
					if !isDel {
						req = meshes.EventsResponse{
							EventType:   meshes.EventType_INFO,
							Summary:     "Successfully deployed k8s component: " + comp.Name,
							OperationId: id.String(),
						}
						msgs = append(msgs, "successfully deployed kubernetes component "+comp.Name)
					} else {
						req = meshes.EventsResponse{
							EventType:   meshes.EventType_INFO,
							Summary:     "Successfully deleted k8s component: " + comp.Name,
							OperationId: id.String(),
						}
						msgs = append(msgs, "successfully deleted kubernetes component "+comp.Name)
					}
					eb.Enqueue(&req)
				}
			}
		}(kcli)
	}
	wg.Wait()
	return strings.Join(msgs, "\n"), mergeErrors(errs)
}

func mergeErrors(errs []error) error {
	var msgs []string

	for _, err := range errs {
		msgs = append(msgs, err.Error())
	}

	if len(msgs) == 0 {
		return nil
	}

	return fmt.Errorf(strings.Join(msgs, "\n"))
}
