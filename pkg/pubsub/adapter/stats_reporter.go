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

package adapter

import (
	"context"
	"strconv"

	"go.opencensus.io/stats/view"
	"knative.dev/pkg/metrics"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"knative.dev/pkg/metrics/metricskey"
)

var (
	// eventCountM is a counter which records the number of events sent.
	eventCountM = stats.Int64(
		"event_count",
		"Number of events sent",
		stats.UnitDimensionless,
	)

	// Create the tag keys that will be used to add tags to our measurements.
	// Tag keys must conform to the restrictions described in
	// go.opencensus.io/tag/validate.go. Currently those restrictions are:
	// - length between 1 and 255 inclusive
	// - characters are printable US-ASCII
	namespaceKey         = tag.MustNewKey(metricskey.LabelNamespaceName)
	eventSourceKey       = tag.MustNewKey(metricskey.LabelEventSource)
	eventTypeKey         = tag.MustNewKey(metricskey.LabelEventType)
	nameKey              = tag.MustNewKey(metricskey.LabelName)
	resourceGroupKey     = tag.MustNewKey(metricskey.LabelResourceGroup)
	responseCodeKey      = tag.MustNewKey(metricskey.LabelResponseCode)
	responseCodeClassKey = tag.MustNewKey(metricskey.LabelResponseCodeClass)
)

type ReportArgs struct {
	Namespace     string
	EventType     string
	EventSource   string
	Name          string
	ResourceGroup string
}

func init() {
	register()
}

// StatsReporter defines the interface for sending metrics.
type StatsReporter interface {
	// ReportEventCount captures the event count. It records one per call.
	ReportEventCount(args *ReportArgs, responseCode int) error
}

var _ StatsReporter = (*reporter)(nil)
var emptyContext = context.Background()

// reporter holds cached metric objects to report metrics.
type reporter struct{}

// NewStatsReporter creates a reporter that collects and reports metrics.
func NewStatsReporter() StatsReporter {
	return &reporter{}
}

func (r *reporter) ReportEventCount(args *ReportArgs, responseCode int) error {
	ctx, err := r.generateTag(args, responseCode)
	if err != nil {
		return err
	}
	metrics.Record(ctx, eventCountM.M(1))
	return nil
}

func (r *reporter) generateTag(args *ReportArgs, responseCode int) (context.Context, error) {
	return tag.New(
		emptyContext,
		tag.Insert(namespaceKey, args.Namespace),
		tag.Insert(eventSourceKey, args.EventSource),
		tag.Insert(eventTypeKey, args.EventType),
		tag.Insert(nameKey, args.Name),
		tag.Insert(resourceGroupKey, args.ResourceGroup),
		tag.Insert(responseCodeKey, strconv.Itoa(responseCode)),
		tag.Insert(responseCodeClassKey, metrics.ResponseCodeClass(responseCode)))
}

func register() {
	tagKeys := []tag.Key{
		namespaceKey,
		eventSourceKey,
		eventTypeKey,
		nameKey,
		resourceGroupKey,
		responseCodeKey,
		responseCodeClassKey}

	// Create view to see our measurements.
	if err := metrics.RegisterResourceView(
		&view.View{
			Description: eventCountM.Description(),
			Measure:     eventCountM,
			Aggregation: view.Count(),
			TagKeys:     tagKeys,
		},
	); err != nil {
		panic(err)
	}
}
