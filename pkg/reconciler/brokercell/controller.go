/*
Copyright 2020 Google LLC

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

// Code generated by injection-gen. DO NOT EDIT.

package brokercell

import (
	"context"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/tools/cache"

	"knative.dev/eventing/pkg/logging"
	deploymentinformer "knative.dev/pkg/client/injection/kube/informers/apps/v1/deployment"
	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	serviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"

	brokerinformer "github.com/google/knative-gcp/pkg/client/injection/informers/broker/v1beta1/broker"
	"github.com/google/knative-gcp/pkg/client/injection/informers/intevents/v1alpha1/brokercell"
	hpainformer "github.com/google/knative-gcp/pkg/client/injection/kube/informers/autoscaling/v2beta2/horizontalpodautoscaler"
	v1alpha1brokercell "github.com/google/knative-gcp/pkg/client/injection/reconciler/intevents/v1alpha1/brokercell"
	"github.com/google/knative-gcp/pkg/reconciler"
	"github.com/google/knative-gcp/pkg/reconciler/brokercell/resources"
)

const (
	// controllerAgentName is the string used by this controller to identify
	// itself when creating events.
	controllerAgentName = "brokercell-controller"
)

// NewController creates a Reconciler for BrokerCell and returns the result of NewImpl.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	logger := logging.FromContext(ctx)

	brokercellInformer := brokercell.Get(ctx)
	brokerLister := brokerinformer.Get(ctx).Lister()
	deploymentLister := deploymentinformer.Get(ctx).Lister()
	svcLister := serviceinformer.Get(ctx).Lister()
	epLister := endpointsinformer.Get(ctx).Lister()
	hpaLister := hpainformer.Get(ctx).Lister()

	base := reconciler.NewBase(ctx, controllerAgentName, cmw)
	r, err := NewReconciler(base, brokerLister, svcLister, epLister, deploymentLister)
	if err != nil {
		logger.Fatal("Failed to create BrokerCell reconciler", zap.Error(err))
	}
	r.hpaLister = hpaLister
	impl := v1alpha1brokercell.NewImpl(ctx, r)

	logger.Info("Setting up event handlers.")

	// TODO(https://github.com/google/knative-gcp/issues/912) Change period back to 5 min once controller
	// watches for data plane components.
	brokercellInformer.Informer().AddEventHandlerWithResyncPeriod(controller.HandleAll(impl.Enqueue), 30*time.Second)

	// Watch data plane components created by brokercell so we can update brokercell status immediately.
	// 1. Watch deployments for ingress, fanout and retry
	deploymentinformer.Get(ctx).Informer().AddEventHandler(handleResourceUpdate(impl))
	// 2. Watch ingress endpoints
	endpointsinformer.Get(ctx).Informer().AddEventHandler(handleResourceUpdate(impl))
	// 3. Watch hpa for ingress, fanout and retry deployments
	hpainformer.Get(ctx).Informer().AddEventHandler(handleResourceUpdate(impl))

	return impl
}

// handleResourceUpdate returns an event handler for resources created by brokercell such as the ingress deployment.
func handleResourceUpdate(impl *controller.Impl) cache.ResourceEventHandler {
	// Since resources created by brokercell live in the same namespace as the brokercell, we use an
	// empty namespaceLabel so that the same namespace of the given object is used to enqueue.
	namespaceLabel := ""
	// Resources created by the brokercell, including the indirectly created ingress service endpoints,
	// have such a label resources.BrokerCellLabelKey=<brokercellName>. Resources without this label
	// will be skipped by the function.
	return controller.HandleAll(impl.EnqueueLabelOfNamespaceScopedResource(namespaceLabel, resources.BrokerCellLabelKey))
}
