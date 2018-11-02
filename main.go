package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/armon/circbuf"
	"github.com/google/go-github/github"
	getter "github.com/hashicorp/go-getter"
)

func downloadGithubRepo(organization string, repo string) {
	client := github.NewClient(nil)
	repository, _, err := client.Repositories.Get(context.Background(), organization, repo)
	if err != nil {
		panic(err)
	}
	g := &getter.GitGetter{}
	url, _ := url.Parse(repository.GetCloneURL())
	g.Get(repo, url)
}

func getAllTerraformResourceFiles(repo string) []string {
	var paths []string
	filepath.Walk(fmt.Sprint("./", repo), func(path string, file os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		isVendorFile := strings.Contains(filepath.Dir(path), "vendor")
		if !isVendorFile && isTerraformResourceFile(file.Name()) {
			paths = append(paths, path)
		}
		return nil
	})
	return paths
}

func listUniqueResources(statePath string, provider string) []string {
	cmdOutput := execTerraformStateList(statePath)
	searchExpression := fmt.Sprintf(`%s_(.*?)[^.]+`, provider)
	re := regexp.MustCompile(searchExpression)
	resourceMatches := re.FindAllString(cmdOutput, -1)
	resourceSet := make(map[string]bool)
	fmt.Printf("matches: %d", len(resourceMatches))
	for _, resource := range resourceMatches {
		resourceSet[resource] = true
	}
	//convert set into slice
	keys := make([]string, 0, len(resourceSet))
	for k := range resourceSet {
		keys = append(keys, k)
	}
	return keys
}

/*
func parseStateFile(statePath string) []string {
	dat, _ := ioutil.ReadFile(statePath)
	var tmpM map[string]interface{}
	json.Unmarshal(dat, &tmpM)
	m := make(map[string][]string, len(tmpM))
	for k, v := range tmpM {
		tmp := v.([]interface{})
		newV := make([]string, len(tmp))
		for i, el := range tmp {
			newV[i] = el.(string)
		}
		m[k] = newV
	}
	return m
}*/

func execTerraformStateList(statePath string) string {
	const maxBufSize = 8 * 1024
	// Execute the command using a shell
	var shell, flag string
	if runtime.GOOS == "windows" {
		shell = "cmd"
		flag = "/C"
	} else {
		shell = "/bin/sh"
		flag = "-c"
	}
	command := fmt.Sprintf("terraform state list -state=%s", statePath)
	cmd := exec.Command(shell, flag, command)
	stdout, _ := circbuf.NewBuffer(maxBufSize)
	stderr, _ := circbuf.NewBuffer(maxBufSize)
	cmd.Stderr = io.Writer(stderr)
	cmd.Stdout = io.Writer(stdout)
	cmd.Run()
	return stdout.String()
}

func generateResourceMap(organization string, provider string) {
	repo := fmt.Sprintf("terraform-provider-%s", provider)
	downloadGithubRepo(organization, repo)
	paths := getAllTerraformResourceFiles(repo)

	re := regexp.MustCompile("(.*):=.*AWSClient.*")
	m := make(map[string][]string)

	for _, path := range paths {
		fmt.Println(path)
		dat, _ := ioutil.ReadFile(path)
		s := string(dat)
		clients := re.FindAllString(s, -1)
		fmt.Printf("clients match: %v\n", clients)
		for _, client := range clients {
			part := strings.Split(client, ":=")[0]
			variableName := strings.TrimSpace(part)
			parts := strings.Split(client, "AWSClient).")

			if len(parts) < 2 {
				fmt.Printf("Unusual client: %s \n", client)
				continue
			}
			serviceName := awsClientMap[parts[1]]
			if len(serviceName) < 1 {
				fmt.Printf("service name cannot be blank\n")
				continue
			}
			searchExpression := fmt.Sprintf(`%s\.(.*?)[^(]+`, variableName)
			searchRegex := regexp.MustCompile(searchExpression)
			fileMatches := searchRegex.FindAllString(s, -1)
			if len(fileMatches) < 1 {
				fmt.Println("No matches")
				continue
			}
			apiSet := make(map[string]bool)
			for _, fileMatch := range fileMatches {
				//trim the first part (e.g.: "conn.")
				api := strings.Replace(fileMatch, variableName+".", "", 1)
				action := fmt.Sprintf("%s:%s", serviceName, api)
				apiSet[action] = true
			}
			//convert set into slice
			keys := make([]string, 0, len(apiSet))
			for k := range apiSet {
				keys = append(keys, k)
			}
			fileName := filepath.Base(path)
			resourceName := strings.Replace(fileName, ".go", "", 1)
			m[resourceName] = keys

		}
	}
	bytes, _ := json.Marshal(m)
	resourceFileName := fmt.Sprintf("%s_resouce.json", provider)
	os.Remove(resourceFileName)
	ioutil.WriteFile(resourceFileName, bytes, 0644)
}

func deserializeResourceMap(provider string) map[string][]string {
	resourceFileName := fmt.Sprintf("%s_resouce.json", provider)
	dat, _ := ioutil.ReadFile(resourceFileName)
	var tmpM map[string]interface{}
	json.Unmarshal(dat, &tmpM)
	m := make(map[string][]string, len(tmpM))
	for k, v := range tmpM {
		tmp := v.([]interface{})
		newV := make([]string, len(tmp))
		for i, el := range tmp {
			newV[i] = el.(string)
		}
		m[k] = newV
	}
	return m
}

func createPolicyDocument(resourceList []string, provider string) string {
	m := deserializeResourceMap(provider)
	actionSet := make(map[string]bool)
	for _, resource := range resourceList {
		fmt.Println("resource: " + resource)
		resourceName := "resource_" + resource
		var actions []string
		if val, ok := m[resourceName]; ok {
			actions = val
		} else {
			resourceName = "data_source_" + resource
			actions = m[resourceName]
		}
		for _, action := range actions {
			actionSet[action] = true
		}
	}
	//convert set into slice
	keys := make([]string, 0, len(actionSet))
	for k := range actionSet {
		keys = append(keys, `"`+k+`"`)
	}
	//sort alphabetically
	sort.Sort(sort.StringSlice(keys))
	policy := "[" + strings.Join(keys, ", ") + "]"
	fmt.Println(policy)
	resourceFileName := fmt.Sprintf("%s_policy.json", provider)
	os.Remove(resourceFileName)
	ioutil.WriteFile(resourceFileName, []byte(policy), 0644)
	return policy
}

func isTerraformResourceFile(fileName string) bool {
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

func main() {
	statePtr := flag.String("state", "state.json", "path to state file")
	providerPtr := flag.String("provider", "aws", "provider to fetch (e.g. aws)")
	useCachePtr := flag.Bool("use-cache", true, "use cache?")
	organizationPtr := flag.String("organization", "terraform-providers", "the github org to fetch provider from")
	flag.Parse()
	statePath := *statePtr
	provider := *providerPtr
	useCache := *useCachePtr
	organization := *organizationPtr
	fmt.Println("state: " + statePath)
	if !useCache {
		generateResourceMap(organization, provider)
	}
	resourceList := listUniqueResources(statePath, provider)
	createPolicyDocument(resourceList, provider)
}

var awsClientMap = map[string]string{
	"cfconn":                "cloudformation",
	"cloud9conn":            "cloud9",
	"cloudfrontconn":        "cloudfront",
	"cloudtrailconn":        "cloudtrail",
	"cloudwatchconn":        "cloudwatch",
	"cloudwatchlogsconn":    "logs",
	"cloudwatcheventsconn":  "events",
	"cognitoconn":           "cognito-identity",
	"cognitoidpconn":        "cognito-idp",
	"configconn":            "config",
	"daxconn":               "dax",
	"devicefarmconn":        "devicefarm",
	"dmsconn":               "dms",
	"dsconn":                "ds",
	"dynamodbconn":          "dynamodb",
	"ec2conn":               "ec2",
	"ecrconn":               "ecr",
	"ecsconn":               "ecs",
	"efsconn":               "elasticfilesystem",
	"eksconn":               "eks",
	"elbconn":               "elasticloadbalancing",
	"elbv2conn":             "elasticloadbalancing",
	"emrconn":               "elasticmapreduce",
	"esconn":                "es",
	"acmconn":               "acm",
	"acmpcaconn":            "acm-pca",
	"apigateway":            "apigateway",
	"appautoscalingconn":    "application-autoscaling",
	"autoscalingconn":       "autoscaling",
	"s3conn":                "s3",
	"secretsmanagerconn":    "secretsmanager",
	"scconn":                "servicecatalog",
	"sesConn":               "ses",
	"simpledbconn":          "sdb",
	"sqsconn":               "sqs",
	"stsconn":               "sts",
	"redshiftconn":          "redshift",
	"r53conn":               "route53",
	"rdsconn":               "rds",
	"iamconn":               "iam",
	"kinesisconn":           "kinesis",
	"kmsconn":               "kms",
	"gameliftconn":          "gamelift",
	"firehoseconn":          "firehose",
	"fmsconn":               "fms",
	"inspectorconn":         "inspector",
	"elasticacheconn":       "elasticache",
	"elasticbeanstalkconn":  "elasticbeanstalk",
	"elastictranscoderconn": "elastictranscoder",
	"lambdaconn":            "lambda",
	"lightsailconn":         "lightsail",
	"macieconn":             "macie",
	"mqconn":                "mq",
	"opsworksconn":          "opsworks",
	"organizationsconn":     "organizations",
	"glacierconn":           "glacier",
	"guarddutyconn":         "guardduty",
	"codebuildconn":         "codebuild",
	"codedeployconn":        "codedeploy",
	"codecommitconn":        "codecommit",
	"codepipelineconn":      "codedeploy",
	"sdconn":                "servicediscovery",
	"sfnconn":               "states",
	"snsconn":               "sns",
	"sqdconn":               "sqs",
	"ssmconn":               "ssm",
	"storagegatewayconn":    "storagegateway",
	"swfconn":               "swf",
	"wafconn":               "waf",
	"wafregionalconn":       "waf-regional",
	"iotconn":               "iot",
	"batchconn":             "batch",
	"glueconn":              "glue",
	"athenaconn":            "athena",
	"dxconn":                "directconnect",
	"mediastoreconn":        "mediastore",
	"appsyncconn":           "appsync",
	"lexmodelconn":          "lex",
	"budgetconn":            "budgets",
	"neptuneconn":           "neptune-db",
	"pricingconn":           "pricing",
	"pinpointconn":          "mobiletargeting",
	"workspacesconn":        "workspaces",
}
