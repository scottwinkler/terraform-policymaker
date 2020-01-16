# terraform-policymaker
This project solves the problem of creating a least priviliged policy for terraform deployments. If you have ever had to sift through logs files to know exactly what your priviliges you need to grant your terraform provider, then you will appreciate this.
## How to use
First build this project using `go build`, then run `./terraform-policymaker -path="<path_to_tf_config>"` to generate a least priviliged policy for your configuration code.
Arguments
* -path: (optional) The path to your Terraform configuration files. Default: ./test
* -provider: (optional) N/A as currently only aws is supported. Default: aws
* -use-cache: (optional) A boolean, to use the cached provider source or not. Default: true
* -organization: (optional) The github organization from which to pull the source code/ Default: terraform-providers
## How does it work?
The key to this entire project is a json file that maps terraform resources to IAM actions. 
Using the `terraform plan` command, we can list the resources that will be created by a terraform deployment and then use a JSON mapping of resource to required permissions to create a least priviliged policy. For example, if we have a terraform deployment that creates a lambda function, then we can do a simple lookup to determine that the following actions will need to be included in the policy:

```
 "resource_aws_lambda_function": [
        "lambda:UpdateFunctionCode",
        "lambda:DeleteFunctionConcurrency",
        "lambda:ListVersionsByFunction",
        "lambda:CreateFunction",
        "lambda:PutFunctionConcurrency",
        "lambda:GetFunction",
        "lambda:DeleteFunction",
        "lambda:UpdateFunctionConfiguration"
    ]
```
By doing a union for all resources in a terraform deployment, a very precise IAM policy can be generated for a given terraform deployment.

So how does this project address the problem of creating an accurate mapping of terraform resources to IAM permissions? By downloading the terraform-provider-aws, using regex to find all API invocations for each resource, determining which IAM action corresponds to that API invocation, and creating a mapping between resource and IAM permissions. Ghetto? Yes. Effective? Also yes/

## Limitations
Currently this only supports creating AWS IAM policies, but it could be extended to support GCP, Azure, or any other terraform provider that offers comprehensive IAM. Additionally, parsing the source code of the providers does result in some errors. It would be better if the individual providers produced their own mapping of resoures to iam actions.

Another problem is that there is inconsistency in the golang sdk for aws such that API invocations do not always correspond nicely to IAM actions, so there are some hardcoded dictionaries to account for these discrepancies.

## Future Improvements
Currently this lists creates a policy that allows actions for all resources. A better policy would scope actions to particular resources, which is definetly possible since we have access to the terraform configuration.

Another thing would be to refine it for just the actions you need for a given deployment. Instead of creating a policy for everything, you really only need permissions for what has changed, and read permissions for everything else.
