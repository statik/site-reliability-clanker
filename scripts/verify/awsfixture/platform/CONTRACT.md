# Fixture of `scripts/verify/aws-bootstrap-destroy-check.sh`

`clanker` reads the source files from a directory that has this file. The stacks in
`stacks/aws` have the layer names and the inputs of the AWS target, but only
`terraform_data` resources and an `s3` backend whose address is the MinIO container of the
check (`AWS_ENDPOINT_URL_S3`). The real contract is `platform/CONTRACT.md` at the root of
the repository.
