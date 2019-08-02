package main

import (
	"flag"

	"github.com/scottwinkler/terraform-policymaker/policymaker"
)

func main() {
	providerPtr := flag.String("provider", "aws", "provider to fetch (e.g. aws)")
	organizationPtr := flag.String("organization", "terraform-providers", "the github org to fetch provider from")
	useCachePtr := flag.Bool("use-cache", true, "if no, then will redownload the provider from GitHub")
	pathPtr := flag.String("path", "./test", "the path to your Terraform configuration code")
	flag.Parse()
	provider := *providerPtr
	organization := *organizationPtr
	useCache := *useCachePtr
	path := *pathPtr

	pm := policymaker.NewPolicyMaker(&policymaker.Options{
		Provider:     provider,
		Organization: organization,
		UseCache:     useCache,
		Path:         path,
	})
	pm.GeneratePolicyDocument()
}
