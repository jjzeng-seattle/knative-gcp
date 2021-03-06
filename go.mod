module github.com/google/knative-gcp

go 1.14

require (
	cloud.google.com/go v0.58.0
	cloud.google.com/go/logging v1.0.1-0.20200331222814-69e77e66e597
	cloud.google.com/go/pubsub v1.3.2-0.20200506222144-2c46308f8465
	cloud.google.com/go/storage v1.8.0
	github.com/cloudevents/sdk-go v1.2.0
	github.com/cloudevents/sdk-go/protocol/pubsub/v2 v2.0.1-0.20200602143929-d07dc0510d45
	github.com/cloudevents/sdk-go/v2 v2.0.1-0.20200608152019-2ab697c8fc0b
	github.com/fsnotify/fsnotify v1.4.9
	github.com/gogo/protobuf v1.3.1
	github.com/golang/protobuf v1.4.2
	github.com/google/go-cmp v0.4.1
	github.com/google/uuid v1.1.1
	github.com/google/wire v0.4.0
	github.com/googleapis/gax-go/v2 v2.0.5
	github.com/kelseyhightower/envconfig v1.4.0
	go.opencensus.io v0.22.4
	go.opentelemetry.io/otel v0.3.0 // indirect
	go.uber.org/multierr v1.5.0
	go.uber.org/zap v1.15.0
	golang.org/x/crypto v0.0.0-20200317142112-1b76d66859c6 // indirect
	golang.org/x/sync v0.0.0-20200317015054-43a5402ce75a
	google.golang.org/api v0.26.0
	google.golang.org/genproto v0.0.0-20200608115520-7c474a2e3482
	google.golang.org/grpc v1.29.1
	google.golang.org/protobuf v1.24.0
	k8s.io/api v0.18.1
	k8s.io/apimachinery v0.18.1
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	knative.dev/eventing v0.15.1-0.20200617151224-2025007875e9
	knative.dev/pkg v0.0.0-20200617175025-6826f2137c31
	knative.dev/serving v0.15.1-0.20200617164325-1b08e8665241
	knative.dev/test-infra v0.0.0-20200617170025-a5ea4989665f // indirect
	sigs.k8s.io/yaml v1.2.0
)

replace (
	istio.io/api => istio.io/api v0.0.0-20200227213531-891bf31f3c32
	istio.io/client-go => istio.io/client-go v0.0.0-20200227214646-23b87b42e49b
	k8s.io/api => k8s.io/api v0.17.6
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.17.6
	k8s.io/apimachinery => k8s.io/apimachinery v0.17.6
	k8s.io/client-go => k8s.io/client-go v0.17.6
	k8s.io/code-generator => k8s.io/code-generator v0.17.6
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20190918143330-0270cf2f1c1d
)

replace github.com/aws/aws-sdk-go => github.com/aws/aws-sdk-go v1.25.1

replace github.com/blang/semver => github.com/blang/semver v1.1.1-0.20190414102917-ba2c2ddd8906

replace github.com/gogo/protobuf => github.com/gogo/protobuf v1.3.0

replace github.com/imdario/mergo => github.com/imdario/mergo v0.3.7

replace github.com/jmespath/go-jmespath => github.com/jmespath/go-jmespath v0.0.0-20180206201540-c2b33e8439af

replace github.com/json-iterator/go => github.com/json-iterator/go v1.1.7

replace github.com/modern-go/reflect2 => github.com/modern-go/reflect2 v0.0.0-20180701023420-4b7aa43c6742

replace github.com/pkg/errors => github.com/pkg/errors v0.8.1

replace github.com/robfig/cron/v3 => github.com/robfig/cron/v3 v3.0.0

replace gomodules.xyz/jsonpatch/v2 => gomodules.xyz/jsonpatch/v2 v2.0.1

replace gopkg.in/yaml.v2 => gopkg.in/yaml.v2 v2.2.2

replace honnef.co/go/tools => honnef.co/go/tools v0.0.1-2019.2.3

replace sigs.k8s.io/yaml => sigs.k8s.io/yaml v1.1.0
