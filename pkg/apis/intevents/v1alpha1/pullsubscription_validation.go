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

package v1alpha1

import (
	"context"
	"time"

	"github.com/google/go-cmp/cmp/cmpopts"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	duckv1 "knative.dev/pkg/apis/duck/v1"

	duckv1alpha1 "github.com/google/knative-gcp/pkg/apis/duck/v1alpha1"

	"github.com/google/go-cmp/cmp"
	"knative.dev/pkg/apis"
)

const (
	minRetentionDuration = 10 * time.Second   // 10 seconds.
	maxRetentionDuration = 7 * 24 * time.Hour // 7 days.

	minAckDeadline = 0 * time.Second  // 0 seconds.
	maxAckDeadline = 10 * time.Minute // 10 minutes.
)

func (current *PullSubscription) Validate(ctx context.Context) *apis.FieldError {
	errs := current.Spec.Validate(ctx).ViaField("spec")
	return duckv1alpha1.ValidateAutoscalingAnnotations(ctx, current.Annotations, errs)
}

func (current *PullSubscriptionSpec) Validate(ctx context.Context) *apis.FieldError {
	var errs *apis.FieldError
	// Topic [required]
	if current.Topic == "" {
		errs = errs.Also(apis.ErrMissingField("topic"))
	}
	// Sink [required]
	if equality.Semantic.DeepEqual(current.Sink, duckv1.Destination{}) {
		errs = errs.Also(apis.ErrMissingField("sink"))
	} else if err := current.Sink.Validate(ctx); err != nil {
		errs = errs.Also(err.ViaField("sink"))
	}
	// Transformer [optional]
	if current.Transformer != nil && !equality.Semantic.DeepEqual(current.Transformer, &duckv1.Destination{}) {
		if err := current.Transformer.Validate(ctx); err != nil {
			errs = errs.Also(err.ViaField("transformer"))
		}
	}

	if current.RetentionDuration != nil {
		// If set, RetentionDuration Cannot be longer than 7 days or shorter than 10 minutes.
		rd, err := time.ParseDuration(*current.RetentionDuration)
		if err != nil {
			errs = errs.Also(apis.ErrInvalidValue(*current.RetentionDuration, "retentionDuration"))
		} else if rd < minRetentionDuration || rd > maxRetentionDuration {
			errs = errs.Also(apis.ErrOutOfBoundsValue(*current.RetentionDuration, minRetentionDuration.String(), maxRetentionDuration.String(), "retentionDuration"))
		}
	}

	if current.AckDeadline != nil {
		// If set, AckDeadline needs to parse to a valid duration.
		ad, err := time.ParseDuration(*current.AckDeadline)
		if err != nil {
			errs = errs.Also(apis.ErrInvalidValue(*current.AckDeadline, "ackDeadline"))
		} else if ad < minAckDeadline || ad > maxAckDeadline {
			errs = errs.Also(apis.ErrOutOfBoundsValue(*current.AckDeadline, minAckDeadline.String(), maxAckDeadline.String(), "ackDeadline"))
		}
	}

	// Mode [optional]
	switch current.Mode {
	case "", ModeCloudEventsBinary, ModeCloudEventsStructured, ModePushCompatible:
		// valid
	default:
		errs = errs.Also(apis.ErrInvalidValue(current.Mode, "mode"))
	}

	if current.Secret != nil {
		if !equality.Semantic.DeepEqual(current.Secret, &corev1.SecretKeySelector{}) {
			err := validateSecret(current.Secret)
			if err != nil {
				errs = errs.Also(err.ViaField("secret"))
			}
		}
	}

	return errs
}

// TODO move this to a common place.
func validateSecret(secret *corev1.SecretKeySelector) *apis.FieldError {
	var errs *apis.FieldError
	if secret.Name == "" {
		errs = errs.Also(apis.ErrMissingField("name"))
	}
	if secret.Key == "" {
		errs = errs.Also(apis.ErrMissingField("key"))
	}
	return errs
}

func (current *PullSubscription) CheckImmutableFields(ctx context.Context, original *PullSubscription) *apis.FieldError {
	if original == nil {
		return nil
	}

	var errs *apis.FieldError
	// Modification of Topic, Secret and Project are not allowed. Everything else is mutable.
	if diff := cmp.Diff(original.Spec, current.Spec,
		cmpopts.IgnoreFields(PullSubscriptionSpec{},
			"Sink", "Transformer", "Mode", "AckDeadline", "RetainAckedMessages", "RetentionDuration", "CloudEventOverrides")); diff != "" {
		errs = errs.Also(&apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: diff,
		})
	}
	// Modification of non-empty cluster name annotation is not allowed.
	return duckv1alpha1.CheckImmutableClusterNameAnnotation(&current.ObjectMeta, &original.ObjectMeta, errs)
}
