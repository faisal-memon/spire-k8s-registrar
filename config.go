package main

import (
	"io/ioutil"

	"github.com/hashicorp/hcl"
	"github.com/zeebo/errs"
)

const (
	defaultLogLevel      = "info"
	defaultMetricsAddr   = ":8080"
	defaultPodController = true
	defaultAddSvcDNSName = true
)

type Config struct {
	Cluster            string   `hcl:"cluster"`
	LogLevel           string   `hcl:"log_level"`
	LogPath            string   `hcl:"log_path"`
	MetricsAddr        string   `hcl:"metrics_addr"`
	ServerSocketPath   string   `hcl:"server_socket_path"`
	ServerAddress      string   `hcl:"server_address"`
	TrustDomain        string   `hcl:"trust_domain"`
	PodController      bool     `hcl:"pod_controller"`
	AddSvcDNSName      bool     `hcl:"add_svc_dns_name"`
	PodLabel           string   `hcl:"pod_label"`
	PodAnnotation      string   `hcl:"pod_annotation"`
	LeaderElection     bool     `hcl:"leader_election"`
	DisabledNamespaces []string `hcl:"disabled_namespaces"`
}

func LoadConfig(path string) (*Config, error) {
	hclBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, errs.New("unable to load configuration: %v", err)
	}
	return ParseConfig(string(hclBytes))
}

func ParseConfig(hclConfig string) (*Config, error) {
	c := new(Config)
	c.PodController = defaultPodController
	c.AddSvcDNSName = defaultAddSvcDNSName
	if err := hcl.Decode(c, hclConfig); err != nil {
		return nil, errs.New("unable to decode configuration: %v", err)
	}

	if c.LogLevel == "" {
		c.LogLevel = defaultLogLevel
	}
	if c.MetricsAddr == "" {
		c.MetricsAddr = defaultMetricsAddr
	}
	if c.DisabledNamespaces == nil {
		c.DisabledNamespaces = defaultNamespaces()
	}
	if c.Cluster == "" {
		return nil, errs.New("cluster must be specified")
	}
	if c.ServerSocketPath == "" {
		return nil, errs.New("server_socket_path must be specified")
	}
	if c.TrustDomain == "" {
		return nil, errs.New("trust_domain must be specified")
	}
	if c.PodLabel != "" && c.PodAnnotation != "" {
		return nil, errs.New("workload registration mode specification is incorrect, can't specify both pod_label and pod_annotation")
	}

	return c, nil
}

func defaultNamespaces() []string {
	return []string{"kube-system"}

}
