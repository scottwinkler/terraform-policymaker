package policymaker

import (
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strings"
)

// PolicyMaker is responsible for creating policy documents
type PolicyMaker struct {
	ProviderParser *ProviderParser
	PlanParser     *PlanParser
}

// Options represents the options for creating a policymaker
type Options struct {
	Provider     string
	Organization string
	UseCache     bool
	Path         string
}

// NewPolicyMaker is the Constructor for PolicyMaker
func NewPolicyMaker(o *Options) *PolicyMaker {
	return &PolicyMaker{
		ProviderParser: NewProviderParser(o.Organization, o.Provider, o.UseCache),
		PlanParser:     NewPlanParser(o.Path),
	}
}

/*
GeneratePolicyDocument Spits out a policy document based on a list of resources that are being used
*/
func (p *PolicyMaker) GeneratePolicyDocument() {
	permissionsMap := p.ProviderParser.GetPermissionsMap()
	resources := p.PlanParser.GetResources()

	fmt.Println("######### New Policy")
	permissionsSet := make(map[string]bool)
	//add permissions to set
	for _, resource := range resources {
		permissions := permissionsMap[resource.ToString()]
		for _, permission := range permissions {
			permissionsSet[permission] = true
		}
	}
	//convert set into slice
	permissionsList := make([]string, 0, len(permissionsSet))
	for permission := range permissionsSet {
		permissionsList = append(permissionsList, `"`+permission+`"`)
	}
	sort.Sort(sort.StringSlice(permissionsList))
	policyActions := "[" + strings.Join(permissionsList, ", ") + "]"
	policy := fmt.Sprintf(`
	{
		"Version": "2012-10-17",
		"Statement": [
			{
				"Effect": "Allow",
				"Action": %s,
				"Resource": "*"
			}
		]
	}
	`, policyActions)
	//Write output to file
	resourceFileName := fmt.Sprintf("%s_policy.json", p.ProviderParser.Provider)
	os.Remove(resourceFileName)
	ioutil.WriteFile(resourceFileName, []byte(policy), 0644)
	fmt.Printf("######### Policy created: %s\n", resourceFileName)
}
