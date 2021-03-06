/*
Copyright 2019 Google LLC

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

package static

import (
	"context"
	"errors"
	"fmt"
	"k8s.io/api/apps/v1"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	clientgotesting "k8s.io/client-go/testing"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"

	"knative.dev/pkg/client/injection/ducks/duck/v1/addressable"
	_ "knative.dev/pkg/client/injection/ducks/duck/v1/addressable/fake"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	logtesting "knative.dev/pkg/logging/testing"
	. "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/resolver"

	duckv1beta1 "github.com/google/knative-gcp/pkg/apis/duck/v1beta1"
	pubsubv1beta1 "github.com/google/knative-gcp/pkg/apis/intevents/v1beta1"
	"github.com/google/knative-gcp/pkg/client/injection/reconciler/intevents/v1beta1/pullsubscription"
	gpubsub "github.com/google/knative-gcp/pkg/gclient/pubsub/testing"
	"github.com/google/knative-gcp/pkg/reconciler"
	"github.com/google/knative-gcp/pkg/reconciler/intevents"
	psreconciler "github.com/google/knative-gcp/pkg/reconciler/intevents/pullsubscription"
	"github.com/google/knative-gcp/pkg/reconciler/intevents/pullsubscription/resources"
	. "github.com/google/knative-gcp/pkg/reconciler/testing"
)

const (
	sourceName      = "source"
	sinkName        = "sink"
	transformerName = "transformer"

	testNS = "testnamespace"

	testImage = "test_image"

	sourceUID = sourceName + "-abc-123"

	testProject = "test-project-id"
	testTopicID = sourceUID + "-TOPIC"
	generation  = 1

	secretName = "testing-secret"

	failedToReconcileSubscriptionMsg = `Failed to reconcile Pub/Sub subscription`
	failedToDeleteSubscriptionMsg    = `Failed to delete Pub/Sub subscription`
)

var (
	sinkDNS = sinkName + ".mynamespace.svc.cluster.local"
	sinkURI = apis.HTTP(sinkDNS)

	transformerDNS = transformerName + ".mynamespace.svc.cluster.local"
	transformerURI = apis.HTTP(transformerDNS)

	sinkGVK = metav1.GroupVersionKind{
		Group:   "testing.cloud.google.com",
		Version: "v1beta1",
		Kind:    "Sink",
	}

	testSubscriptionID = fmt.Sprintf("cre-ps_%s_%s_%s", testNS, sourceName, sourceUID)

	transformerGVK = metav1.GroupVersionKind{
		Group:   "testing.cloud.google.com",
		Version: "v1beta1",
		Kind:    "Transformer",
	}

	secret = corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: secretName,
		},
		Key: "testing-key",
	}
)

func init() {
	// Add types to scheme
	_ = pubsubv1beta1.AddToScheme(scheme.Scheme)
}

func newSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNS,
			Name:      secretName,
		},
		Data: map[string][]byte{
			"testing-key": []byte("abcd"),
		},
	}
}

func newSink() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "testing.cloud.google.com/v1beta1",
			"kind":       "Sink",
			"metadata": map[string]interface{}{
				"namespace": testNS,
				"name":      sinkName,
			},
			"status": map[string]interface{}{
				"address": map[string]interface{}{
					"url": sinkURI.String(),
				},
			},
		},
	}
}

func newSinkDestination(namespace string) duckv1.Destination {
	return duckv1.Destination{
		Ref: &duckv1.KReference{
			APIVersion: "testing.cloud.google.com/v1beta1",
			Kind:       "Sink",
			Name:       sinkName,
			Namespace:  namespace,
		},
	}
}

func newTransformer() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "testing.cloud.google.com/v1beta1",
			"kind":       "Transformer",
			"metadata": map[string]interface{}{
				"namespace": testNS,
				"name":      transformerName,
			},
			"status": map[string]interface{}{
				"address": map[string]interface{}{
					"url": transformerURI.String(),
				},
			},
		},
	}
}

func TestAllCases(t *testing.T) {
	table := TableTest{{
		Name: "bad workqueue key",
		// Make sure Reconcile handles bad keys.
		Key: "too/many/parts",
	}, {
		Name: "key not found",
		// Make sure Reconcile handles good keys that don't exist.
		Key: "foo/not-found",
	}, {
		Name: "cannot get sink",
		Objects: []runtime.Object{
			NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithPullSubscriptionSink(sinkGVK, sinkName),
			),
			newSecret(),
		},
		Key: testNS + "/" + sourceName,
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", sourceName),
			Eventf(corev1.EventTypeWarning, "InvalidSink",
				`InvalidSink: failed to get ref &ObjectReference{Kind:Sink,Namespace:testnamespace,Name:sink,UID:,APIVersion:testing.cloud.google.com/v1beta1,ResourceVersion:,FieldPath:,}: sinks.testing.cloud.google.com "sink" not found`),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchFinalizers(testNS, sourceName, resourceGroup),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionStatusObservedGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithPullSubscriptionSink(sinkGVK, sinkName),
				// updates
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionSinkNotFound(),
			),
		}},
	}, {
		Name: "create client fails",
		Objects: []runtime.Object{
			NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSink(sinkURI),
			),
			newSink(),
			newSecret(),
		},
		Key: testNS + "/" + sourceName,
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", sourceName),
			Eventf(corev1.EventTypeWarning, "SubscriptionReconcileFailed", "Failed to reconcile Pub/Sub subscription: client-create-induced-error"),
		},
		OtherTestData: map[string]interface{}{
			"ps": gpubsub.TestClientData{
				CreateClientErr: errors.New("client-create-induced-error"),
			},
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionStatusObservedGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionProjectID(testProject),
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSink(sinkURI),
				WithPullSubscriptionMarkNoTransformer("TransformerNil", "Transformer is nil"),
				WithPullSubscriptionTransformerURI(nil),
				WithPullSubscriptionMarkNoSubscription("SubscriptionReconcileFailed", fmt.Sprintf("%s: %s", failedToReconcileSubscriptionMsg, "client-create-induced-error"))),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchFinalizers(testNS, sourceName, resourceGroup),
		},
	}, {
		Name: "topic exists fails",
		Objects: []runtime.Object{
			NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSink(sinkURI),
			),
			newSink(),
			newSecret(),
		},
		Key: testNS + "/" + sourceName,
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", sourceName),
			Eventf(corev1.EventTypeWarning, "SubscriptionReconcileFailed", "Failed to reconcile Pub/Sub subscription: topic-exists-induced-error"),
		},
		OtherTestData: map[string]interface{}{
			"ps": gpubsub.TestClientData{
				TopicData: gpubsub.TestTopicData{
					ExistsErr: errors.New("topic-exists-induced-error"),
				},
			},
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchFinalizers(testNS, sourceName, resourceGroup),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionStatusObservedGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionProjectID(testProject),
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSink(sinkURI),
				WithPullSubscriptionMarkNoTransformer("TransformerNil", "Transformer is nil"),
				WithPullSubscriptionTransformerURI(nil),
				WithPullSubscriptionMarkNoSubscription("SubscriptionReconcileFailed", fmt.Sprintf("%s: %s", failedToReconcileSubscriptionMsg, "topic-exists-induced-error"))),
		}},
	}, {
		Name: "topic does not exist",
		Objects: []runtime.Object{
			NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSink(sinkURI),
			),
			newSink(),
			newSecret(),
		},
		Key: testNS + "/" + sourceName,
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", sourceName),
			Eventf(corev1.EventTypeWarning, "SubscriptionReconcileFailed", "Failed to reconcile Pub/Sub subscription: Topic %q does not exist", testTopicID),
		},
		OtherTestData: map[string]interface{}{
			"ps": gpubsub.TestClientData{
				TopicData: gpubsub.TestTopicData{
					Exists: false,
				},
			},
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchFinalizers(testNS, sourceName, resourceGroup),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionStatusObservedGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionProjectID(testProject),
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSink(sinkURI),
				WithPullSubscriptionMarkNoTransformer("TransformerNil", "Transformer is nil"),
				WithPullSubscriptionTransformerURI(nil),
				WithPullSubscriptionMarkNoSubscription("SubscriptionReconcileFailed", fmt.Sprintf("%s: Topic %q does not exist", failedToReconcileSubscriptionMsg, testTopicID))),
		}},
	}, {
		Name: "subscription exists fails",
		Objects: []runtime.Object{
			NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSink(sinkURI),
			),
			newSink(),
			newSecret(),
		},
		Key: testNS + "/" + sourceName,
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", sourceName),
			Eventf(corev1.EventTypeWarning, "SubscriptionReconcileFailed", "Failed to reconcile Pub/Sub subscription: subscription-exists-induced-error"),
		},
		OtherTestData: map[string]interface{}{
			"ps": gpubsub.TestClientData{
				SubscriptionData: gpubsub.TestSubscriptionData{
					ExistsErr: errors.New("subscription-exists-induced-error"),
				},
			},
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchFinalizers(testNS, sourceName, resourceGroup),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionStatusObservedGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionProjectID(testProject),
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSink(sinkURI),
				WithPullSubscriptionMarkNoTransformer("TransformerNil", "Transformer is nil"),
				WithPullSubscriptionTransformerURI(nil),
				WithPullSubscriptionMarkNoSubscription("SubscriptionReconcileFailed", fmt.Sprintf("%s: %s", failedToReconcileSubscriptionMsg, "subscription-exists-induced-error"))),
		}},
	}, {
		Name: "create subscription fails",
		Objects: []runtime.Object{
			NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSink(sinkURI),
			),
			newSink(),
			newSecret(),
		},
		Key: testNS + "/" + sourceName,
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", sourceName),
			Eventf(corev1.EventTypeWarning, "SubscriptionReconcileFailed", "Failed to reconcile Pub/Sub subscription: subscription-create-induced-error"),
		},
		OtherTestData: map[string]interface{}{
			"ps": gpubsub.TestClientData{
				TopicData: gpubsub.TestTopicData{
					Exists: true,
				},
				CreateSubscriptionErr: errors.New("subscription-create-induced-error"),
			},
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchFinalizers(testNS, sourceName, resourceGroup),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionStatusObservedGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionProjectID(testProject),
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSink(sinkURI),
				WithPullSubscriptionMarkNoTransformer("TransformerNil", "Transformer is nil"),
				WithPullSubscriptionTransformerURI(nil),
				WithPullSubscriptionMarkNoSubscription("SubscriptionReconcileFailed", fmt.Sprintf("%s: %s", failedToReconcileSubscriptionMsg, "subscription-create-induced-error"))),
		}},
	}, {
		Name: "successfully created subscription",
		Objects: []runtime.Object{
			NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSink(sinkURI),
			),
			newSink(),
			newSecret(),
		},
		Key: testNS + "/" + sourceName,
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", sourceName),
			Eventf(corev1.EventTypeNormal, "PullSubscriptionReconciled", `PullSubscription reconciled: "%s/%s"`, testNS, sourceName),
		},
		OtherTestData: map[string]interface{}{
			"ps": gpubsub.TestClientData{
				TopicData: gpubsub.TestTopicData{
					Exists: true,
				},
			},
		},
		WantCreates: []runtime.Object{
			newReceiveAdapter(context.Background(), testImage, nil),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionProjectID(testProject),
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSink(sinkURI),
				WithPullSubscriptionMarkNoTransformer("TransformerNil", "Transformer is nil"),
				WithPullSubscriptionTransformerURI(nil),
				// Updates
				WithPullSubscriptionStatusObservedGeneration(generation),
				WithPullSubscriptionMarkSubscribed(testSubscriptionID),
				WithPullSubscriptionMarkNoDeployed(deploymentName(), testNS),
			),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchFinalizers(testNS, sourceName, resourceGroup),
		},
	}, {
		Name: "sink namespace empty, default to the source one",
		Objects: []runtime.Object{
			NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
						SourceSpec: duckv1.SourceSpec{
							Sink: newSinkDestination(""),
						},
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSink(sinkURI),
			),
			newSink(),
			newSecret(),
		},
		Key: testNS + "/" + sourceName,
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", sourceName),
			Eventf(corev1.EventTypeNormal, "PullSubscriptionReconciled", `PullSubscription reconciled: "%s/%s"`, testNS, sourceName),
		},
		OtherTestData: map[string]interface{}{
			"ps": gpubsub.TestClientData{
				TopicData: gpubsub.TestTopicData{
					Exists: true,
				},
			},
		},
		WantCreates: []runtime.Object{
			newReceiveAdapter(context.Background(), testImage, nil),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionProjectID(testProject),
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSink(sinkURI),
				WithPullSubscriptionMarkNoTransformer("TransformerNil", "Transformer is nil"),
				WithPullSubscriptionTransformerURI(nil),
				// Updates
				WithPullSubscriptionStatusObservedGeneration(generation),
				WithPullSubscriptionMarkSubscribed(testSubscriptionID),
				WithPullSubscriptionMarkNoDeployed(deploymentName(), testNS),
			),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchFinalizers(testNS, sourceName, resourceGroup),
		},
	}, {
		Name: "sink URI set instead of ref",
		Objects: []runtime.Object{
			NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
						SourceSpec: duckv1.SourceSpec{
							Sink: duckv1.Destination{
								URI: sinkURI,
							},
						},
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSink(sinkURI),
			),
			newSink(),
			newSecret(),
		},
		Key: testNS + "/" + sourceName,
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", sourceName),
			Eventf(corev1.EventTypeNormal, "PullSubscriptionReconciled", `PullSubscription reconciled: "%s/%s"`, testNS, sourceName),
		},
		OtherTestData: map[string]interface{}{
			"ps": gpubsub.TestClientData{
				TopicData: gpubsub.TestTopicData{
					Exists: true,
				},
			},
		},
		WantCreates: []runtime.Object{
			newReceiveAdapter(context.Background(), testImage, nil),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionProjectID(testProject),
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSink(sinkURI),
				WithPullSubscriptionMarkNoTransformer("TransformerNil", "Transformer is nil"),
				WithPullSubscriptionTransformerURI(nil),
				// Updates
				WithPullSubscriptionStatusObservedGeneration(generation),
				WithPullSubscriptionMarkSubscribed(testSubscriptionID),
				WithPullSubscriptionMarkNoDeployed(deploymentName(), testNS),
			),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchFinalizers(testNS, sourceName, resourceGroup),
		},
	}, {
		Name: "successful create - reuse existing receive adapter - match",
		Objects: []runtime.Object{
			NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithPullSubscriptionSink(sinkGVK, sinkName),
			),
			newSink(),
			newSecret(),
			newAvailableReceiveAdapter(context.Background(), testImage, nil),
		},
		OtherTestData: map[string]interface{}{
			"ps": gpubsub.TestClientData{
				TopicData: gpubsub.TestTopicData{
					Exists: true,
				},
			},
		},
		Key: testNS + "/" + sourceName,
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", sourceName),
			Eventf(corev1.EventTypeNormal, "PullSubscriptionReconciled", `PullSubscription reconciled: "%s/%s"`, testNS, sourceName),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchFinalizers(testNS, sourceName, resourceGroup),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionProjectID(testProject),
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSubscribed(testSubscriptionID),
				WithPullSubscriptionMarkDeployed(deploymentName(), testNS),
				WithPullSubscriptionMarkSink(sinkURI),
				WithPullSubscriptionMarkNoTransformer("TransformerNil", "Transformer is nil"),
				WithPullSubscriptionTransformerURI(nil),
				WithPullSubscriptionStatusObservedGeneration(generation),
			),
		}},
	}, {
		Name: "successful create - reuse existing receive adapter - mismatch",
		Objects: []runtime.Object{
			NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionTransformer(transformerGVK, transformerName),
			),
			newSink(),
			newTransformer(),
			newSecret(),
			newReceiveAdapter(context.Background(), "old"+testImage, nil),
		},
		OtherTestData: map[string]interface{}{
			"ps": gpubsub.TestClientData{
				TopicData: gpubsub.TestTopicData{
					Exists: true,
				},
			},
		},
		Key: testNS + "/" + sourceName,
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", sourceName),
			Eventf(corev1.EventTypeNormal, "PullSubscriptionReconciled", `PullSubscription reconciled: "%s/%s"`, testNS, sourceName),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: testNS,
				Verb:      "update",
				Resource:  receiveAdapterGVR(),
			},
			Object: newReceiveAdapter(context.Background(), testImage, transformerURI),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchFinalizers(testNS, sourceName, resourceGroup),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				//WithPullSubscriptionFinalizers(resourceGroup),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithInitPullSubscriptionConditions,
				WithPullSubscriptionProjectID(testProject),
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionTransformer(transformerGVK, transformerName),
				WithPullSubscriptionMarkSubscribed(testSubscriptionID),
				WithPullSubscriptionMarkNoDeployed(deploymentName(), testNS),
				WithPullSubscriptionMarkSink(sinkURI),
				WithPullSubscriptionMarkTransformer(transformerURI),
				WithPullSubscriptionStatusObservedGeneration(generation),
			),
		}},
	}, {
		Name: "deleting - failed to delete subscription",
		Objects: []runtime.Object{
			NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSubscribed(testSubscriptionID),
				WithPullSubscriptionMarkDeployed(deploymentName(), testNS),
				WithPullSubscriptionMarkSink(sinkURI),
				WithPullSubscriptionDeleted,
			),
			newSecret(),
		},
		OtherTestData: map[string]interface{}{
			"ps": gpubsub.TestClientData{
				TopicData: gpubsub.TestTopicData{
					Exists: true,
				},
				SubscriptionData: gpubsub.TestSubscriptionData{
					Exists:    true,
					DeleteErr: errors.New("subscription-delete-induced-error"),
				},
			},
		},
		Key: testNS + "/" + sourceName,
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "SubscriptionDeleteFailed", "Failed to delete Pub/Sub subscription: subscription-delete-induced-error"),
		},
		WantStatusUpdates: nil,
	}, {
		Name: "successfully deleted subscription",
		Objects: []runtime.Object{
			NewPullSubscription(sourceName, testNS,
				WithPullSubscriptionUID(sourceUID),
				WithPullSubscriptionObjectMetaGeneration(generation),
				WithPullSubscriptionStatusObservedGeneration(generation),
				WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
					PubSubSpec: duckv1beta1.PubSubSpec{
						Secret:  &secret,
						Project: testProject,
					},
					Topic: testTopicID,
				}),
				WithPullSubscriptionSink(sinkGVK, sinkName),
				WithPullSubscriptionMarkSubscribed(testSubscriptionID),
				WithPullSubscriptionMarkDeployed(deploymentName(), testNS),
				WithPullSubscriptionMarkSink(sinkURI),
				WithPullSubscriptionDeleted,
			),
			newSecret(),
		},
		OtherTestData: map[string]interface{}{
			"ps": gpubsub.TestClientData{
				TopicData: gpubsub.TestTopicData{
					Exists: true,
				},
				SubscriptionData: gpubsub.TestSubscriptionData{
					Exists: true,
				},
			},
		},
		Key:        testNS + "/" + sourceName,
		WantEvents: nil,
	}}

	defer logtesting.ClearAll()
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher, testData map[string]interface{}) controller.Reconciler {
		ctx = addressable.WithDuck(ctx)
		pubsubBase := &intevents.PubSubBase{
			Base: reconciler.NewBase(ctx, controllerAgentName, cmw),
		}
		r := &Reconciler{
			Base: &psreconciler.Base{
				PubSubBase:             pubsubBase,
				DeploymentLister:       listers.GetDeploymentLister(),
				PullSubscriptionLister: listers.GetPullSubscriptionLister(),
				UriResolver:            resolver.NewURIResolver(ctx, func(types.NamespacedName) {}),
				ReceiveAdapterImage:    testImage,
				CreateClientFn:         gpubsub.TestClientCreator(testData["ps"]),
				ControllerAgentName:    controllerAgentName,
				ResourceGroup:          resourceGroup,
			},
		}
		r.ReconcileDataPlaneFn = r.ReconcileDeployment
		return pullsubscription.NewReconciler(ctx, r.Logger, r.RunClientSet, listers.GetPullSubscriptionLister(), r.Recorder, r)
	}))
}

func deploymentName() string {
	ps := newPullSubscription()
	return resources.GenerateReceiveAdapterName(ps)
}

func newReceiveAdapter(ctx context.Context, image string, transformer *apis.URL) runtime.Object {
	ps := newPullSubscription()
	args := &resources.ReceiveAdapterArgs{
		Image:            image,
		PullSubscription: ps,
		Labels:           resources.GetLabels(controllerAgentName, sourceName),
		SubscriptionID:   testSubscriptionID,
		SinkURI:          sinkURI,
		TransformerURI:   transformer,
	}
	ra := resources.MakeReceiveAdapter(ctx, args)
	return ra
}

func newAvailableReceiveAdapter(ctx context.Context, image string, transformer *apis.URL) runtime.Object {
	obj := newReceiveAdapter(ctx, image, transformer)
	ra := obj.(*v1.Deployment)
	WithDeploymentAvailable()(ra)
	return obj
}

func newPullSubscription() *pubsubv1beta1.PullSubscription {
	return NewPullSubscription(sourceName, testNS,
		WithPullSubscriptionUID(sourceUID),
		WithPullSubscriptionSpec(pubsubv1beta1.PullSubscriptionSpec{
			PubSubSpec: duckv1beta1.PubSubSpec{
				Secret:  &secret,
				Project: testProject,
			},
			Topic: testTopicID,
		}))
}

func receiveAdapterGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployment",
	}
}

func patchFinalizers(namespace, name, finalizer string, existingFinalizers ...string) clientgotesting.PatchActionImpl {
	action := clientgotesting.PatchActionImpl{}
	action.Name = name
	action.Namespace = namespace

	for i, ef := range existingFinalizers {
		existingFinalizers[i] = fmt.Sprintf("%q", ef)
	}
	if finalizer != "" {
		existingFinalizers = append(existingFinalizers, fmt.Sprintf("%q", finalizer))
	}
	fname := strings.Join(existingFinalizers, ",")
	patch := `{"metadata":{"finalizers":[` + fname + `],"resourceVersion":""}}`
	action.Patch = []byte(patch)
	return action
}
