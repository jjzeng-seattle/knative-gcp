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

package ingress

import (
	"context"
	"fmt"
	"sync"

	"cloud.google.com/go/pubsub"
	"go.opencensus.io/trace"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"

	cepubsub "github.com/cloudevents/sdk-go/protocol/pubsub/v2"
	cev2 "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/binding"
	"github.com/cloudevents/sdk-go/v2/extensions"
	"github.com/cloudevents/sdk-go/v2/protocol"
	"github.com/google/knative-gcp/pkg/broker/config"
	"knative.dev/eventing/pkg/logging"
)

const projectEnvKey = "PROJECT_ID"

// NewMultiTopicDecoupleSink creates a new multiTopicDecoupleSink.
func NewMultiTopicDecoupleSink(ctx context.Context, brokerConfig config.ReadonlyTargets, client *pubsub.Client) *multiTopicDecoupleSink {
	return &multiTopicDecoupleSink{
		logger:       logging.FromContext(ctx),
		pubsub:       client,
		brokerConfig: brokerConfig,
		// TODO(#1118): remove Topic when broker config is removed
		topics: make(map[types.NamespacedName]*pubsub.Topic),
	}
}

// multiTopicDecoupleSink implements DecoupleSink and routes events to pubsub topics corresponding
// to the broker to which the events are sent.
type multiTopicDecoupleSink struct {
	// pubsub talks to pubsub.
	pubsub *pubsub.Client
	// map from brokers to topics
	topics    map[types.NamespacedName]*pubsub.Topic
	topicsMut sync.RWMutex
	// brokerConfig holds configurations for all brokers. It's a view of a configmap populated by
	// the broker controller.
	brokerConfig config.ReadonlyTargets
	logger       *zap.Logger
}

// Send sends incoming event to its corresponding pubsub topic based on which broker it belongs to.
func (m *multiTopicDecoupleSink) Send(ctx context.Context, ns, broker string, event cev2.Event) protocol.Result {
	topic, err := m.getTopicForBroker(types.NamespacedName{Namespace: ns, Name: broker})
	if err != nil {
		return err
	}

	dt := extensions.FromSpanContext(trace.FromContext(ctx).SpanContext())
	msg := new(pubsub.Message)
	if err := cepubsub.WritePubSubMessage(ctx, binding.ToMessage(&event), msg, dt.WriteTransformer()); err != nil {
		return err
	}

	_, err = topic.Publish(ctx, msg).Get(ctx)
	return err
}

// getTopicForBroker finds the corresponding decouple topic for the broker from the mounted broker configmap volume.
func (m *multiTopicDecoupleSink) getTopicForBroker(broker types.NamespacedName) (*pubsub.Topic, error) {
	topicID, err := m.getTopicIDForBroker(broker)
	if err != nil {
		return nil, err
	}

	if topic, ok := m.getExistingTopic(broker); ok {
		// Check that the broker's topic ID hasn't changed.
		if topic.ID() == topicID {
			return topic, nil
		}
	}

	// Topic needs to be created or updated.
	return m.updateTopicForBroker(broker)
}

func (m *multiTopicDecoupleSink) updateTopicForBroker(broker types.NamespacedName) (*pubsub.Topic, error) {
	m.topicsMut.Lock()
	defer m.topicsMut.Unlock()
	// Fetch latest decouple topic ID under lock.
	topicID, err := m.getTopicIDForBroker(broker)
	if err != nil {
		return nil, err
	}

	if topic, ok := m.topics[broker]; ok {
		if topic.ID() == topicID {
			// Topic already updated.
			return topic, nil
		}
		// Stop old topic.
		m.topics[broker].Stop()
	}
	topic := m.pubsub.Topic(topicID)
	m.topics[broker] = topic
	return topic, nil
}

func (m *multiTopicDecoupleSink) getTopicIDForBroker(broker types.NamespacedName) (string, error) {
	brokerConfig, ok := m.brokerConfig.GetBroker(broker.Namespace, broker.Name)
	if !ok {
		// There is an propagation delay between the controller reconciles the broker config and
		// the config being pushed to the configmap volume in the ingress pod. So sometimes we return
		// an error even if the request is valid.
		m.logger.Warn("config is not found for", zap.String("broker", broker.String()))
		return "", fmt.Errorf("%q: %w", broker, ErrNotFound)
	}
	if brokerConfig.State != config.State_READY {
		m.logger.Debug("broker is not ready", zap.Any("ns", broker.Namespace), zap.Any("broker", broker))
		return "", fmt.Errorf("%q: %w", broker, ErrNotReady)
	}
	if brokerConfig.DecoupleQueue == nil || brokerConfig.DecoupleQueue.Topic == "" {
		m.logger.Error("DecoupleQueue or topic missing for broker, this should NOT happen.", zap.Any("brokerConfig", brokerConfig))
		return "", fmt.Errorf("decouple queue of %q: %w", broker, ErrIncomplete)
	}
	return brokerConfig.DecoupleQueue.Topic, nil
}

func (m *multiTopicDecoupleSink) getExistingTopic(broker types.NamespacedName) (*pubsub.Topic, bool) {
	m.topicsMut.RLock()
	defer m.topicsMut.RUnlock()
	topic, ok := m.topics[broker]
	return topic, ok
}
