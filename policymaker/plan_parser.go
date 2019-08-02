package policymaker

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/tidwall/gjson"
)

const (
	tfplanExt            = "tfplan"
	tfplanStdoutFilename = "terraform-plan.stdout"
	tfplanJSONFilename   = "terraform-plan.json"
)

// PlanParser downloads and parses the source code for a given provider
type PlanParser struct {
	Path string
}

// NewPlanParser is the constructor for ProviderParser
func NewPlanParser(path string) *PlanParser {
	return &PlanParser{
		Path: path,
	}
}

/*
GetResources gets all unique resources in a plan file
*/
func (p *PlanParser) GetResources() []*Resource {
	plan := p.getPlanAsJSON()
	rootModule := gjson.Get(plan, "configuration.root_module").String()
	resources := p.getModuleResources(rootModule)
	return p.removeDuplicates(resources)
}

func (p *PlanParser) getPlanAsJSON() string {
	fmt.Printf("Getting plan as JSON\n")
	// change to folder where configuration code is in
	cwd, _ := os.Getwd()
	os.Chdir(p.Path)

	if !exists(tfplanJSONFilename) {
		fmt.Printf("Plan does not exist, creating new onen")
		// run a terraform init
		command := "terraform init"
		execCmd(command)

		// run a terraform plan and save the file in a temporary file
		command = fmt.Sprintf("terraform plan -out=%s", tfplanStdoutFilename)
		execCmd(command)

		// convert the plan into JSON
		command = fmt.Sprintf("terraform show -json %s > %s", tfplanStdoutFilename, tfplanJSONFilename)
		execCmd(command)

		//clean up
		os.Remove(tfplanStdoutFilename)
	}

	dat, _ := ioutil.ReadFile(tfplanJSONFilename)
	os.Chdir(cwd)
	json := string(dat)
	return json
}

func (p *PlanParser) getModuleResources(module string) []*Resource {
	var resources []*Resource
	result := gjson.Get(module, "resources")
	result.ForEach(func(key, value gjson.Result) bool {
		t := value.Get("type").String()
		m := value.Get("mode").String()
		resources = append(resources, NewResource(t, m))
		return true
	})
	// has more?
	moduleCalls := gjson.Get(module, "module_calls")
	if moduleCalls.Exists() {
		//recusively call this method again for each child module
		moduleCalls.ForEach(func(key, value gjson.Result) bool {
			resources = append(resources, p.getModuleResources(value.Get("module").String())...)
			return true
		})
	}
	return resources
}

func (p *PlanParser) removeDuplicates(resources []*Resource) []*Resource {
	resourceSet := make(map[*Resource]bool)
	for _, resource := range resources {
		resourceSet[resource] = true
	}
	//convert set into slice
	resourceList := make([]*Resource, 0, len(resourceSet))
	for key := range resourceSet {
		resourceList = append(resourceList, key)
	}
	return resourceList
}
