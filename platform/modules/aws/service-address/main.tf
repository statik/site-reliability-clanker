# The address of the load balancer of a Service of type LoadBalancer. The AWS service
# controller writes it into the status of the Service some time after the Service exists,
# and neither Kubernetes nor Helm waits for it. So the first apply waits for it: the script
# uses the tools of the runner image (aws and kubectl) and the keys of the job. A later plan
# reads the Service at plan time.

locals {
  namespace = split("/", var.service)[0]
  name      = split("/", var.service)[1]
}

resource "terraform_data" "wait" {
  triggers_replace = [var.service]

  provisioner "local-exec" {
    interpreter = ["bash"]
    command     = "${path.module}/wait-for-address.sh"

    environment = {
      CLUSTER_NAME     = var.cluster_name
      CLUSTER_ENDPOINT = var.cluster_endpoint
      CLUSTER_CA_DATA  = var.cluster_ca_data
      REGION           = var.region
      NAMESPACE        = local.namespace
      SERVICE          = local.name
    }
  }
}

data "kubernetes_service_v1" "this" {
  metadata {
    name      = local.name
    namespace = local.namespace
  }

  depends_on = [terraform_data.wait]
}

locals {
  ingress = try(data.kubernetes_service_v1.this.status[0].load_balancer[0].ingress[0], {})
  address = try(compact([try(local.ingress.hostname, ""), try(local.ingress.ip, "")])[0], "")
}
