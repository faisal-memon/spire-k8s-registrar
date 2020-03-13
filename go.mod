module github.com/transferwise/spire-k8s-registrar

go 1.13

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/go-logr/logr v0.1.0
	github.com/golang/groupcache v0.0.0-20190129154638-5b532d6fd5ef // indirect
	github.com/hashicorp/hcl v1.0.0
	github.com/onsi/ginkgo v1.10.1
	github.com/onsi/gomega v1.7.0
	github.com/prometheus/client_golang v1.0.0 // indirect
	github.com/spiffe/go-spiffe v0.0.0-20200115174642-4e401e3b85fe
	github.com/spiffe/spire/proto/spire v0.9.2
	github.com/zeebo/errs v1.2.2
	go.uber.org/atomic v1.4.0 // indirect
	go.uber.org/zap v1.10.0 // indirect
	golang.org/x/crypto v0.0.0-20190923035154-9ee001bba392 // indirect
	google.golang.org/grpc v1.24.0
	k8s.io/api v0.17.0
	k8s.io/apimachinery v0.17.0
	k8s.io/client-go v0.17.0
	sigs.k8s.io/controller-runtime v0.4.0
)

replace github.com/transferwise/spire-k8s-registrar => ./
