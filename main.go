package main

import (
	"bufio"
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
	fmt.Printf("Downloading %s repo from github\n", repo)
	client := github.NewClient(nil)
	repository, _, err := client.Repositories.Get(context.Background(), organization, repo)
	if err != nil {
		panic(err)
	}
	g := &getter.GitGetter{}
	url, _ := url.Parse(repository.GetCloneURL())
	g.Get(repo, url)
}

/*This method reads the provider from a local folder and returns a list of all terraform
resource and terrafrom data resource files
*/
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

/*This method reads a state file given by a path and returns a slice of all the
terraform resources it uses (for the given provider, based on a cache).
*/
func listUniqueResources(statePath string, provider string) []string {
	cmdOutput := execTerraformStateList(statePath)
	searchExpression := fmt.Sprintf(`%s_(.*?)[^.]+`, provider)
	re := regexp.MustCompile(searchExpression)
	resourceMatches := re.FindAllString(cmdOutput, -1)
	resourceSet := make(map[string]bool)
	for _, resource := range resourceMatches {
		fmt.Println(resource)
		resourceSet[resource] = true
	}
	//convert set into slice
	keys := make([]string, 0, len(resourceSet))
	for k := range resourceSet {
		keys = append(keys, k)
	}
	return keys
}

/*Wrapper around the "terraform state list" command. Could be modified to also handle
"terraform graph", which could be useful if turning this into a proper terraform provider
*/
func execTerraformStateList(statePath string) string {
	const maxBufSize = 16 * 1024
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

/*Rebuild a cache for mapping terraform resource names to actions. This should
not be run often, it is usually enough to simply use the cached json file.
*/
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
		//fmt.Printf("clients match: %v\n", clients)
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
			//add "hidden" actions for each resource of a service
			if actions, ok := awsHiddenActionsMap[serviceName]; ok {
				for _, action := range actions {
					apiSet[action] = true
				}
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
	//Write the output to a file for caching
	bytes, _ := json.Marshal(m)
	resourceFileName := fmt.Sprintf("%s_resouce_mapping.json", provider)
	os.Remove(resourceFileName)
	ioutil.WriteFile(resourceFileName, bytes, 0644)
}

/*Parse the cached file into a golang map[string][]string
 */
func deserializeResourceMap(provider string) map[string][]string {
	resourceFileName := fmt.Sprintf("%s_resouce_mapping.json", provider)
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

/*Spit out a policy document based on a list of resources that are being used
 */
func createPolicyDocument(resourceList []string, provider string) string {
	m := deserializeResourceMap(provider)
	actionSet := make(map[string]bool)
	fmt.Println("######### New Policy")

	//add common actions that will be needed for every policy
	for _, action := range awsCommonActions {
		actionSet[action] = true
	}

	for _, resource := range resourceList {
		//fmt.Println("resource: " + resource)
		resourceName := "resource_" + resource
		dataResourceName := "data_source_" + resource

		var actions []string
		resourceActions, rOk := m[resourceName]
		dataResourceActions, dOk := m[dataResourceName]
		//Terraform state list does not make a distinction between data resources and regular resource
		if rOk && dOk {
			fmt.Printf("Indeterminate: " + resource + "\n")
			validText := false
			scanner := bufio.NewScanner(os.Stdin)
			for !validText {
				fmt.Print("Select [r]esource or [d]ata resource: ")
				scanner.Scan()
				text := scanner.Text()
				if text == "r" {
					actions = resourceActions
					validText = true
				} else if text == "d" {
					actions = dataResourceActions
					validText = true
				}
			}
		} else if rOk {
			actions = resourceActions
		} else if dOk {
			actions = dataResourceActions
		}
		for _, action := range actions {
			//is this an add action that we need to account for?
			if val, ok := awsIdiosyncracyActionMap[action]; ok {
				if len(val) > 0 {
					for _, v := range val {
						actionSet[v] = true
					}
				}
				continue
			}
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
	policyActions := "[" + strings.Join(keys, ", ") + "]"
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
	resourceFileName := fmt.Sprintf("%s_policy.json", provider)
	os.Remove(resourceFileName)
	ioutil.WriteFile(resourceFileName, []byte(policy), 0644)
	fmt.Printf("######### Policy created: %s\n", resourceFileName)
	return policy
}

//helper function for validating that a file is a terraform resource or data resource
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
	if !useCache {
		fmt.Println("invalidating cache")
		generateResourceMap(organization, provider)
	}
	resourceList := listUniqueResources(statePath, provider)
	createPolicyDocument(resourceList, provider)
}

/*a list of actions that should be included for all deployments.
These represent the set of actions that are non resource-specific
and therefore cannot be known simply by parsing the state file
*/
var awsCommonActions = []string{
	//
}

/*actions that need to be included for particular services
but is not exactly clear to which resource they belong. Additional
research could be done to find out exactly where these should go */
var awsHiddenActionsMap = map[string][]string{
	"ec2":     []string{"ec2:DescribeVpcAttribute", "ec2:DescribeRouteTables", "ec2:DescribeAccountAttributes"},
	"route53": []string{"route53:GetChange"},
	"iam":     []string{"iam:PassRole"},
	"lambda":  []string{"lambda:ListVersionsByFunction"},
	"s3":      []string{"s3:GetBucketTagging"},
}

/*The naming of the api methods does not map always to sensible iam
actions. This map is used to resolve some of the inconsistencies of the aws sdk
*/
var awsIdiosyncracyActionMap = map[string][]string{
	"apigateway:CreateAuthorizer":           []string{"apigateway:*"},
	"apigateway:CreateModel":                []string{"apigateway:*"},
	"apigateway:CreateResource":             []string{"apigateway:*"},
	"apigateway:CreateRestApi":              []string{"apigateway:*"},
	"apigateway:DeleteAuthorizer":           []string{"apigateway:*"},
	"apigateway:DeleteGatewayResponse":      []string{"apigateway:*"},
	"apigateway:DeleteIntegration":          []string{"apigateway:*"},
	"apigateway:DeleteIntegrationResponse":  []string{"apigateway:*"},
	"apigateway:DeleteMethod":               []string{"apigateway:*"},
	"apigateway:DeleteMethodResponse":       []string{"apigateway:*"},
	"apigateway:DeleteModel":                []string{"apigateway:*"},
	"apigateway:DeleteResource":             []string{"apigateway:*"},
	"apigateway:DeleteRestApi":              []string{"apigateway:*"},
	"apigateway:GetAuthorizer":              []string{"apigateway:*"},
	"apigateway:GetGatewayResponse":         []string{"apigateway:*"},
	"apigateway:GetIntegration":             []string{"apigateway:*"},
	"apigateway:GetIntegrationResponse":     []string{"apigateway:*"},
	"apigateway:GetMethod":                  []string{"apigateway:*"},
	"apigateway:GetMethodResponse":          []string{"apigateway:*"},
	"apigateway:GetModel":                   []string{"apigateway:*"},
	"apigateway:GetResource":                []string{"apigateway:*"},
	"apigateway:GetResourcesPages":          []string{"apigateway:*"},
	"apigateway:GetRestApi":                 []string{"apigateway:*"},
	"apigateway:PutGatewayResponse":         []string{"apigateway:*"},
	"apigateway:PutIntegration":             []string{"apigateway:*"},
	"apigateway:PutIntegrationResponse":     []string{"apigateway:*"},
	"apigateway:PutMethod":                  []string{"apigateway:*"},
	"apigateway:PutMethodResponse":          []string{"apigateway:*"},
	"apigateway:PutRestApi":                 []string{"apigateway:*"},
	"apigateway:UpdateAuthorizer":           []string{"apigateway:*"},
	"apigateway:UpdateIntegration":          []string{"apigateway:*"},
	"apigateway:UpdateMethod":               []string{"apigateway:*"},
	"apigateway:UpdateMethodResponse":       []string{"apigateway:*"},
	"apigateway:UpdateModel":                []string{"apigateway:*"},
	"apigateway:UpdateResource":             []string{"apigateway:*"},
	"apigateway:UpdateRestApi":              []string{"apigateway:*"},
	"apigateway:CreateBasePathMapping":      []string{"apigateway:*"},
	"apigateway:CreateDeployment":           []string{"apigateway:*"},
	"apigateway:CreateDomainName":           []string{"apigateway:*"},
	"apigateway:DeleteBasePathMapping":      []string{"apigateway:*"},
	"apigateway:DeleteDeployment":           []string{"apigateway:*"},
	"apigateway:DeleteDomainName":           []string{"apigateway:*"},
	"apigateway:DeleteStage":                []string{"apigateway:*"},
	"apigateway:GetAccount":                 []string{"apigateway:*"},
	"apigateway:GetBasePathMapping":         []string{"apigateway:*"},
	"apigateway:GetDeployment":              []string{"apigateway:*"},
	"apigateway:GetDomainName":              []string{"apigateway:*"},
	"apigateway:GetStage":                   []string{"apigateway:*"},
	"apigateway:UpdateAccount":              []string{"apigateway:*"},
	"apigateway:UpdateDeployment":           []string{"apigateway:*"},
	"apigateway:UpdateDomainName":           []string{"apigateway:*"},
	"cloudfront:CreateDistributionWithTags": []string{"cloudfront:TagResource", "cloudfront:CreateDistribution", "cloudfront:CreateDistributionWithTags"},
	"iam:ListAttachedRolePoliciesPages":     []string{"iam:ListAttachedRolePolicies"},
	"iam:ListEntitiesForPolicyPages":        []string{"iam:ListEntitiesForPolicy"},
	"iam:ListRolePoliciesPages":             []string{"iam:ListRolePolicies"},
	"route53:ListResourceRecordSetsPages":   []string{"route53:ListResourceRecordSets"},
	"s3:DeleteBucketCors":                   []string{},
	"s3:DeleteBucketEncryption":             []string{},
	"s3:DeleteBucketLifecycle":              []string{},
	"s3:DeleteBucketReplication":            []string{},
	"s3:DeleteObjects":                      []string{"s3:DeleteObject"},
	"s3:GetBucketAccelerateConfiguration":   []string{"s3:GetAccelerateConfiguration"},
	"s3:GetBucketEncryption":                []string{"s3:GetEncryptionConfiguration"},
	"s3:GetBucketLifecycleConfiguration":    []string{"s3:GetLifecycleConfiguration"},
	"s3:GetBucketReplication":               []string{"s3:GetReplicationConfiguration"},
	"s3:HeadObject":                         []string{"s3:GetObject"},
	"s3:HeadBucket":                         []string{"s3:HeadBucket", "s3:ListBucket"},
	"s3:ListObjectVersions":                 []string{"s3:GetObjectVersion"},
	"s3:PutBucketAccelerateConfiguration":   []string{"s3:PutAccelerateConfiguration"},
	"s3:PutBucketEncryption":                []string{"s3:PutEncryptionConfiguration"},
	"s3:PutBucketLifecycleConfiguration":    []string{"s3:PutLifecycleConfiguration"},
	"s3:PutBucketReplication":               []string{"s3:PutReplicationConfiguration"},
}

/* This maps the connection client object instantianted in the terraform-provider-aws
to the name of the service, which will be used to construct the appropriate iam action
*/
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
