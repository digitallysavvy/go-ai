package bedrock

import (
	"fmt"
	"os"
	"strings"
)

// awsPartitionDNSSuffix maps a region prefix to the DNS suffix used for
// non-standard AWS partitions. Ports TS resolve-amazon-bedrock-base-url.ts.
type awsPartitionDNSSuffix struct {
	RegionPrefix string
	DNSSuffix    string
}

var awsPartitionDNSSuffixes = []awsPartitionDNSSuffix{
	{"cn-", "amazonaws.com.cn"},
	{"us-iso-", "c2s.ic.gov"},
	{"us-isob-", "sc2s.sgov.gov"},
	{"eu-isoe-", "cloud.adc-e.uk"},
	{"us-isof-", "csp.hci.ic.gov"},
	{"eusc-", "amazonaws.eu"},
}

// ResolveBaseURLOptions configures ResolveAmazonBedrockBaseURL. Exported so
// it can be shared across Bedrock sub-packages — the Converse client
// (pkg/providers/bedrock) and Bedrock-Anthropic
// (pkg/providers/bedrock/anthropic) both call ResolveAmazonBedrockBaseURL
// with their own service name and endpoint env var, rather than each
// hard-coding "https://{service}.{region}.amazonaws.com".
type ResolveBaseURLOptions struct {
	// BaseURL is the explicit override, if any. Always wins.
	BaseURL string
	// Region is the AWS region used to build the default endpoint and to
	// select a non-standard partition DNS suffix.
	Region string
	// Service is "bedrock-runtime" or "bedrock-agent-runtime".
	Service string
	// ServiceEndpointURLEnvironmentVarName is the service-specific endpoint
	// override environment variable, checked before the generic
	// AWS_ENDPOINT_URL.
	ServiceEndpointURLEnvironmentVarName string
}

// ResolveAmazonBedrockBaseURL ports TS
// resolve-amazon-bedrock-base-url.ts#resolveAmazonBedrockBaseURL. Precedence:
// explicit BaseURL, then the service-specific endpoint env var, then the
// generic AWS_ENDPOINT_URL, then a generated
// https://{service}.{region}.{suffix} URL (suffix depends on the region's
// partition). The result never has a trailing slash.
func ResolveAmazonBedrockBaseURL(opts ResolveBaseURLOptions) (string, error) {
	resolved := opts.BaseURL
	if resolved == "" {
		resolved = os.Getenv(opts.ServiceEndpointURLEnvironmentVarName)
	}
	if resolved == "" {
		resolved = os.Getenv("AWS_ENDPOINT_URL")
	}
	if resolved != "" {
		return strings.TrimRight(resolved, "/"), nil
	}

	if opts.Region == "" {
		return "", fmt.Errorf("AWS region is required: set Region or AWS_REGION")
	}

	dnsSuffix := "amazonaws.com"
	for _, partition := range awsPartitionDNSSuffixes {
		if strings.HasPrefix(opts.Region, partition.RegionPrefix) {
			dnsSuffix = partition.DNSSuffix
			break
		}
	}

	return fmt.Sprintf("https://%s.%s.%s", opts.Service, opts.Region, dnsSuffix), nil
}
