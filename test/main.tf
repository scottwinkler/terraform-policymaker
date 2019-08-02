provider "aws" {
    region = "us-west-2"
    profile = "em-transit-dev-admin"
}

resource "aws_ecr_repository" "foo" {
  name = "bar"
}

module "a" {
    source = "./modules/a"
}