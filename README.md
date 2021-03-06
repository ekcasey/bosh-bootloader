# bosh-bootloader
---

This is a command line utility for standing up a CloudFoundry or Concourse installation 
on an IAAS. This CLI is currently under heavy development, and the initial goal is to 
support bootstrapping a CloudFoundry installation on AWS.

* [CI](https://mega.ci.cf-app.com/pipelines/bosh-bootloader)
* [Tracker](https://www.pivotaltracker.com/n/projects/1488988)

## Prerequisites

### Install Dependencies

The following should be installed on your local machine
- Golang >= 1.5 (install with `brew install go`)
- bosh-init ([installation instructions](http://bosh.io/docs/install-bosh-init.html))

NOTE: Golang 1.7 is highly recommended

### Install bosh-bootloader

bosh-bootloader can be installed with go get:

```
go get github.com/cloudfoundry/bosh-bootloader/bbl
```

### Configure AWS

The AWS IAM user that is provided to bbl will need the following policy:

```
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "ec2:*",
                "cloudformation:*",
                "elasticloadbalancing:*",
                "iam:*"
            ],
            "Resource": [
                "*"
            ]
        }
    ]
}
```

## Usage

The `bbl` command can be invoked on the command line and will display its usage.

```
$ bbl
Usage:
  bbl [GLOBAL OPTIONS] COMMAND [OPTIONS]

Global Options:
  --help      [-h]       Print usage
  --version   [-v]       Print version
  --state-dir            Directory containing bbl-state.json

Commands:
  create-lbs             Attaches load balancer(s)
  delete-lbs             Deletes attached load balancer(s)
  destroy                Tears down BOSH director infrastructure
  director-address       Prints BOSH director address
  director-username      Prints BOSH director username
  director-password      Prints BOSH director password
  director-ca-cert       Prints BOSH director CA certificate
  env-id                 Prints environment ID
  help                   Prints usage
  lbs                    Prints attached load balancer(s)
  ssh-key                Prints SSH private key
  up                     Deploys BOSH director on AWS
  update-lbs             Updates load balancer(s)
  version                Prints version

  Use "bbl [command] --help" for more information about a command.
```
