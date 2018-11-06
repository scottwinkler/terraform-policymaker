# terraform-policymaker
This project solves the problem of creating a least priviliged policy for terraform deployments. If you have ever had to sift through logs files to know exactly what your priviliges you need to grant your terraform provider, then you will appreciate this.
## How to use
First build this project using `govendor sync` followed by `go build`, then run `./terraform-policymaker -state=PATH_TO_STATEFILE` to generate a least priviliged policy for your state file.
Arguments
* -state: (optional) the path to the state file you wish to process. Default: state.json
* -provider: (optional) N/A as currently only aws is supported. Default: aws
* -use-cache: (optional) A boolean, to use the cached resource mapping or not. Default: true
* -organization: (optional) The github organization from which to pull the source code/ Default: terraform-providers
## How does it work?
The key to this entire project is a json file that maps terraform resources to iam actions. 
Using the `terraform state list` command, we can list the resources in a terraform deployment and then use the json mapping to create a least priviliged policy. For example, if we have a terraform deployment that creates a lambda function, then we can do a simple lookup to determine that the following actions will need to be included in the policy:

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
By doing a union for all resources in a terraform deployment, a very precise iam policy can be generated for a given terraform deployment.

So how does this project address the problem of creating an accurate mapping of terraform resources to iam policies? The short answer is ghetto code and plenty of duct tape and glue. The long answer is by downloading the terraform-provider-aws, using regex to find all api invocations for each resource, determining which iam action corresponds to that api invocation, and creating a mapping between resource and iam actions.

## Limitations
Currently this only supports creating AWS IAM policies, but it could be extended to support GCP, Azure, or any other terraform provider that offers comprehensive IAM. Additionally, parsing the source code of the providers does result in some errors. It would be better if the individual providers produced their own mapping of resoures to iam actions.

Another problem is that there is inconsistency in the golang sdk for aws such that api invocations do not always correspond nicely to iam actions, so there are some hardcoded dictionaries to account for these discrepancies.

Finally, `terraform state list` is kind of a shitty command in that it doesn't make a distinction between resources and data resources, so there is a prompt for the user to select whether a resource is a resource or a data resource.

## Future Improvements
Currently this lists creates a policy that allows actions for all resources. A better policy would scope actions to particular resources, which is definetly possible since we have access to the terraform configuration.

Another problem is that you first need to do a succesful deployment in order to acquire a state file. It would be better if we used to "terraform graph" command to list all resources in the terraform plan so that a policy could be generated without having to do a deployment first. This conveniently also solves the issue of `terraform state list` not being able to determine wether a resource is a resource or a data resource.