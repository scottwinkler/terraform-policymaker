package policymaker

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/go-github/github"
	getter "github.com/hashicorp/go-getter"
)

// ProviderParser downloads and parses the source code for a given provider
type ProviderParser struct {
	Organization string
	Provider     string
	Repo         string
	UseCache     bool
	OutputFile   string
}

// NewProviderParser is the constructor for ProviderParser
func NewProviderParser(org string, provider string, useCache bool) *ProviderParser {
	return &ProviderParser{
		Organization: org,
		Provider:     provider,
		Repo:         fmt.Sprintf("terraform-provider-%s", provider),
		UseCache:     useCache,
		OutputFile:   fmt.Sprintf("%s_resouce_mapping.json", provider),
	}
}

// GetPermissionsMap will generate and read a permissions map, if not
func (p *ProviderParser) GetPermissionsMap() map[string][]string {
	if !p.UseCache || !exists(p.Repo) {
		p.downloadGithubRepo()
		p.generatePermissionsMap()
	}
	//if the permissions map doesn't exist, then create it
	if !exists(p.OutputFile) {
		p.generatePermissionsMap()
	}
	return p.readPermissionsMap()
}

/*
This method downloads all the provider source code from GitHub into the local directory
*/
func (p *ProviderParser) downloadGithubRepo() {
	fmt.Printf("Downloading %s repo from github\n", p.Repo)
	client := github.NewClient(nil)
	repository, _, err := client.Repositories.Get(context.Background(), p.Organization, p.Repo)
	if err != nil {
		panic(err)
	}
	g := &getter.GitGetter{}
	url, _ := url.Parse(repository.GetCloneURL())
	g.Get(p.Repo, url)
}

/*
Rebuild a cache for mapping terraform resource names to permissions. This should
not be run often, it is usually enough to simply use the cached json file.
*/
func (p *ProviderParser) generatePermissionsMap() {
	fmt.Printf("Generating permissions map\n")
	paths := p.getAllResourceFiles()

	// currently only AWS is supported
	re := regexp.MustCompile("(.*):=.*AWSClient.*")
	permissionsMap := make(map[string][]string)

	//for each resource file, find all API calls and add those permissions to the map
	for _, path := range paths {
		dat, _ := ioutil.ReadFile(path)
		s := string(dat)
		//clients will be all lines that contain ".*AWSClient.*"
		clients := re.FindAllString(s, -1)

		for _, client := range clients {
			//fmt.Printf("%s\n", client)
			part := strings.Split(client, ":=")[0]
			variableName := strings.TrimSpace(part)
			parts := strings.Split(client, "AWSClient).")

			// this means its the AWS Client itself
			if len(parts) < 2 {
				continue
			}
			connectionName := parts[1]

			// serviceName will be something like ec2 or s3 or lambda
			serviceName := awsClientMap[connectionName]
			if len(serviceName) < 1 {
				//ignore region - false positive
				if connectionName == "region" {
					continue
				}
				//fmt.Printf("service name cannot be blank\n")
				continue
			}
			searchExpression := fmt.Sprintf(`%s\.(.*?)[^(]+`, variableName)
			searchRegex := regexp.MustCompile(searchExpression)
			fileMatches := searchRegex.FindAllString(s, -1)
			if len(fileMatches) < 1 {
				fmt.Printf("No matches\n")
				continue
			}

			//add required permissions to permission set
			permissionsSet := make(map[string]bool)
			for _, fileMatch := range fileMatches {
				// trim the first part (e.g.: "conn.")
				action := strings.Replace(fileMatch, variableName+".", "", 1)
				permission := fmt.Sprintf("%s:%s", serviceName, action)
				permissionsSet[permission] = true
			}

			//convert set into slice
			permissionsList := make([]string, 0, len(permissionsSet))
			for k := range permissionsSet {
				permissionsList = append(permissionsList, k)
			}
			resourceName := strings.Replace(filepath.Base(path), ".go", "", 1)
			permissionsMap[resourceName] = permissionsList
		}
	}
	//Write the output to a file for caching
	bytes, _ := json.Marshal(permissionsMap)
	os.Remove(p.OutputFile)
	ioutil.WriteFile(p.OutputFile, bytes, 0644)
}

/*
Parse the cached file into a golang map[string][]string
*/
func (p *ProviderParser) readPermissionsMap() map[string][]string {
	dat, _ := ioutil.ReadFile(p.OutputFile)
	var tmpM map[string]interface{}
	json.Unmarshal(dat, &tmpM)
	permissionsMap := make(map[string][]string, len(tmpM))
	for k, v := range tmpM {
		tmp := v.([]interface{})
		newV := make([]string, len(tmp))
		for i, el := range tmp {
			newV[i] = el.(string)
		}
		permissionsMap[k] = newV
	}
	return permissionsMap
}

/*
This method reads the provider source code from a local folder and returns a list of all
terraform resource and terrafrom data source files
*/
func (p *ProviderParser) getAllResourceFiles() []string {
	var paths []string
	filepath.Walk(fmt.Sprint("./", p.Repo), func(path string, file os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		isVendorFile := strings.Contains(filepath.Dir(path), "vendor")
		if !isVendorFile && p.isTerraformResourceFile(file.Name()) {
			paths = append(paths, path)
		}
		return nil
	})
	return paths
}

//helper function for validating that a file is a terraform resource or data resource
func (p *ProviderParser) isTerraformResourceFile(fileName string) bool {
	//must be a go file
	if !strings.HasSuffix(fileName, ".go") {
		return false
	}
	//test files not allowed
	if strings.HasSuffix(fileName, "test.go") {
		return false
	}

	//and have a prefix that either either a data resource of resource
	if strings.HasPrefix(fileName, "data_source") || strings.HasPrefix(fileName, "resource") {
		return true
	}
	//otherwise it is invalid
	return false
}
