/*

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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/spiffe/go-spiffe/spiffe"
	"github.com/spiffe/spire/proto/spire/api/registration"
	spiffeidv1beta1 "github.com/transferwise/spire-k8s-registrar/api/v1beta1"
	"github.com/transferwise/spire-k8s-registrar/controllers"
	uzap "go.uber.org/zap"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

var (
	scheme     = runtime.NewScheme()
	setupLog   = ctrl.Log.WithName("setup")
	configFlag = flag.String("config", "spire-k8s-registrar.conf", "configuration file")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = spiffeidv1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	flag.Parse()

	config, err := LoadConfig(*configFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%+v\n", err)
		os.Exit(1)
	}

	atomicLogLevel := ToZap(config.LogLevel)
	ctrl.SetLogger(zap.New(func(o *zap.Options) {
		o.Level = &atomicLogLevel
	}))

	//Connect to Spire Server
	spireClient, err := ConnectSpire(context.Background(), setupLog, config.ServerAddress, config.ServerSocketPath)
	if err != nil {
		setupLog.Error(err, "unable to connect to spire server")
		os.Exit(1)
	}
	setupLog.Info("Connected to SPIRE Server.", "Socket", "unix://"+config.ServerSocketPath)

	// Setup all Controllers
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: config.MetricsAddr,
		LeaderElection:     config.LeaderElection,
		Port:               9443,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.SpiffeIDReconciler{
		Client:      mgr.GetClient(),
		Log:         ctrl.Log.WithName("controllers").WithName("SpiffeID"),
		Scheme:      mgr.GetScheme(),
		SpireClient: spireClient,
		Cluster:     config.Cluster,
		TrustDomain: config.TrustDomain,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "SpiffeID")
		os.Exit(1)
	}
	if config.PodController {
		mode := controllers.PodReconcilerModeServiceAccount
		value := ""
		if len(config.PodLabel) > 0 {
			mode = controllers.PodReconcilerModeLabel
			value = config.PodLabel
		}
		if len(config.PodAnnotation) > 0 {
			mode = controllers.PodReconcilerModeAnnotation
			value = config.PodAnnotation
		}
		ctlr := &controllers.PodController{
			Client:             mgr.GetClient(),
			Mgr:                mgr,
			Log:                ctrl.Log.WithName("controllers"),
			Scheme:             mgr.GetScheme(),
			TrustDomain:        config.TrustDomain,
			Mode:               mode,
			Value:              value,
			DisabledNamespaces: config.DisabledNamespaces,
			AddSvcDNSName:      config.AddSvcDNSName,
		}
		if err := controllers.BuildPodControllers(ctlr); err != nil {
			setupLog.Error(err, "unable to create controller")
			os.Exit(1)
		}
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

type SpiffeLogWrapper struct {
	delegate logr.Logger
}

func (slw SpiffeLogWrapper) Debugf(format string, args ...interface{}) {
	slw.delegate.V(1).Info(fmt.Sprintf(format, args...))
}
func (slw SpiffeLogWrapper) Infof(format string, args ...interface{}) {
	slw.delegate.Info(fmt.Sprintf(format, args...))
}
func (slw SpiffeLogWrapper) Warnf(format string, args ...interface{}) {
	slw.delegate.Info(fmt.Sprintf(format, args...))
}
func (slw SpiffeLogWrapper) Errorf(format string, args ...interface{}) {
	slw.delegate.Info(fmt.Sprintf(format, args...))
}

func ConnectSpire(ctx context.Context, log logr.Logger, serverAddress, serverSocketPath string) (registration.RegistrationClient, error) {

	var conn *grpc.ClientConn
	var err error

	if serverAddress != "" {
		tlsPeer, err := spiffe.NewTLSPeer(spiffe.WithWorkloadAPIAddr("unix://"+serverSocketPath), spiffe.WithLogger(SpiffeLogWrapper{log}))
		if err != nil {
			return nil, err
		}
		conn, err = tlsPeer.DialGRPC(ctx, serverAddress, spiffe.ExpectAnyPeer())
		if err != nil {
			return nil, err
		}
	} else {
		conn, err = grpc.DialContext(ctx, "unix://"+serverSocketPath, grpc.WithInsecure())
		if err != nil {
			return nil, err
		}
	}
	spireClient := registration.NewRegistrationClient(conn)
	return spireClient, nil
}

func ToZap(level string) uzap.AtomicLevel {
	switch level {
	case "fatal":
		return uzap.NewAtomicLevelAt(uzap.FatalLevel)
	case "panic":
		return uzap.NewAtomicLevelAt(uzap.PanicLevel)
	case "error":
		return uzap.NewAtomicLevelAt(uzap.ErrorLevel)
	case "warn":
		return uzap.NewAtomicLevelAt(uzap.WarnLevel)
	case "warning":
		return uzap.NewAtomicLevelAt(uzap.WarnLevel)
	case "info":
		return uzap.NewAtomicLevelAt(uzap.InfoLevel)
	case "debug":
		return uzap.NewAtomicLevelAt(uzap.DebugLevel)
	default:
		return uzap.NewAtomicLevelAt(uzap.InfoLevel)
	}
}
