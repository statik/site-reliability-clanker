# The settings of the core are SecureString parameters below one path, encrypted with a
# customer key. CLANKER_PARAMETER_PATH in the image of the core names the path.
#
# The core reads only the parameters directly below the path (internal/awsparams), and each
# of them must be a setting of the core. The settings of Keycloak are one level deeper.
# The permission boundary of the agent roles denies the reading of /src and of every
# parameter below it.

locals {
  parameter_path     = "/src/${var.env_name}"
  idp_parameter_path = "${local.parameter_path}/idp"
  parameter_arn      = "arn:${local.partition}:ssm:${local.arn_suffix}:parameter"
  ssm_service        = "ssm.${var.region}.amazonaws.com"
}

resource "random_password" "client_secret" {
  length  = 40
  special = false
}

resource "random_password" "user" {
  for_each = toset(["admin", "approver"])

  length  = 24
  special = false
}

resource "tls_private_key" "signing" {
  algorithm = "ED25519"
}

locals {
  # The core reads its signing key as a Go ed25519.PrivateKey in base64: the 32-byte seed,
  # then the 32-byte public key. A PKCS #8 Ed25519 key is a 16-byte header and the seed. A
  # SubjectPublicKeyInfo is a 12-byte header and the public key. OpenTofu has no byte type,
  # so the bytes pass through ISO-8859-1 text, which has one character for each byte.
  signing_der = {
    for part, pem in {
      private = tls_private_key.signing.private_key_pem_pkcs8
      public  = tls_private_key.signing.public_key_pem
    } : part => textdecodebase64(replace(pem, "/-----[^-]+-----|\\s/", ""), "ISO-8859-1")
  }
  signing_der_headers = {
    private = textencodebase64(regex("(?s)^(.{16})", local.signing_der.private)[0], "ISO-8859-1")
    public  = textencodebase64(regex("(?s)^(.{12})", local.signing_der.public)[0], "ISO-8859-1")
  }
  signing_key = textencodebase64(join("", [
    regex("(?s)^.{16}(.{32})$", local.signing_der.private)[0],
    regex("(?s)^.{12}(.{32})$", local.signing_der.public)[0],
  ]), "ISO-8859-1")
}

locals {
  admin_email    = "admin@${local.idp_host}"
  approver_email = "approver@${local.idp_host}"

  core_settings = {
    CLANKER_DATABASE_URL       = local.database_url
    CLANKER_SIGNING_KEY        = local.signing_key
    CLANKER_PUBLIC_URL         = local.core_url
    CLANKER_OIDC_ISSUER        = local.oidc_issuer
    CLANKER_OIDC_CLIENT_ID     = local.client_id
    CLANKER_OIDC_CLIENT_SECRET = random_password.client_secret.result
    CLANKER_ADMIN_EMAIL        = local.admin_email
    CLANKER_AWS_RUNNER         = local.runner_setting
  }
  # A for_each key cannot come from a map with secret values, so the names are a list.
  core_setting_names = toset(concat(
    ["CLANKER_DATABASE_URL", "CLANKER_SIGNING_KEY", "CLANKER_PUBLIC_URL", "CLANKER_OIDC_ISSUER",
    "CLANKER_OIDC_CLIENT_ID", "CLANKER_OIDC_CLIENT_SECRET", "CLANKER_ADMIN_EMAIL"],
    var.runner_image_digest == null ? [] : ["CLANKER_AWS_RUNNER"],
  ))

  idp_settings = {
    CLANKER_OIDC_CLIENT_SECRET = random_password.client_secret.result
    REALM_ADMIN_PASSWORD       = random_password.user["admin"].result
    REALM_APPROVER_PASSWORD    = random_password.user["approver"].result
  }
  idp_setting_names = toset(["CLANKER_OIDC_CLIENT_SECRET", "REALM_ADMIN_PASSWORD",
  "REALM_APPROVER_PASSWORD"])
}

data "aws_iam_policy_document" "settings_key" {
  # The account manages the key, so that the key never becomes unmanageable. No statement
  # lets an IAM policy use the key: applier-30-data has kms:Decrypt on every key.
  statement {
    sid    = "AccountManagesTheKey"
    effect = "Allow"
    actions = [
      "kms:CancelKeyDeletion",
      "kms:CreateAlias",
      "kms:DeleteAlias",
      "kms:DescribeKey",
      "kms:DisableKey",
      "kms:DisableKeyRotation",
      "kms:EnableKey",
      "kms:EnableKeyRotation",
      "kms:GetKeyPolicy",
      "kms:GetKeyRotationStatus",
      "kms:ListAliases",
      "kms:ListKeyPolicies",
      "kms:ListResourceTags",
      "kms:PutKeyPolicy",
      "kms:ScheduleKeyDeletion",
      "kms:TagResource",
      "kms:UntagResource",
      "kms:UpdateAlias",
      "kms:UpdateKeyDescription",
    ]
    resources = ["*"]

    principals {
      type        = "AWS"
      identifiers = ["arn:${local.partition}:iam::${local.account_id}:root"]
    }
  }

  # The role that applies this stack writes the parameters, and the provider reads each one
  # back with decryption.
  statement {
    sid       = "OperatorWritesTheSettings"
    effect    = "Allow"
    actions   = ["kms:Decrypt", "kms:Encrypt"]
    resources = ["*"]

    principals {
      type        = "AWS"
      identifiers = [data.aws_iam_session_context.operator.issuer_arn]
    }

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = [local.ssm_service]
    }
  }

  statement {
    sid       = "CoreReadsItsSettings"
    effect    = "Allow"
    actions   = ["kms:Decrypt"]
    resources = ["*"]

    principals {
      type        = "AWS"
      identifiers = [aws_iam_role.core.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = [local.ssm_service]
    }

    condition {
      test     = "StringLike"
      variable = "kms:EncryptionContext:PARAMETER_ARN"
      values   = ["${local.parameter_arn}${local.parameter_path}/*"]
    }
  }

  statement {
    sid       = "KeycloakReadsItsOwnSettings"
    effect    = "Allow"
    actions   = ["kms:Decrypt"]
    resources = ["*"]

    principals {
      type        = "AWS"
      identifiers = [aws_iam_role.keycloak_execution.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = [local.ssm_service]
    }

    condition {
      test     = "StringLike"
      variable = "kms:EncryptionContext:PARAMETER_ARN"
      values   = ["${local.parameter_arn}${local.idp_parameter_path}/*"]
    }
  }
}

resource "aws_kms_key" "settings" {
  description             = "Settings of the S.R.C. core ${var.env_name} in Parameter Store"
  deletion_window_in_days = 7
  enable_key_rotation     = true
  policy                  = data.aws_iam_policy_document.settings_key.json
}

resource "aws_kms_alias" "settings" {
  name          = "alias/${local.name}-settings"
  target_key_id = aws_kms_key.settings.key_id
}

resource "aws_ssm_parameter" "core" {
  for_each = local.core_setting_names

  name   = "${local.parameter_path}/${each.key}"
  type   = "SecureString"
  key_id = aws_kms_key.settings.arn
  value  = local.core_settings[each.key]

  lifecycle {
    precondition {
      condition = each.key != "CLANKER_SIGNING_KEY" || (
        local.signing_der_headers.private == "MC4CAQAwBQYDK2VwBCIEIA==" &&
        local.signing_der_headers.public == "MCowBQYDK2VwAyEA"
      )
      error_message = "The Ed25519 key is not in the PKCS #8 and SPKI layout of signing_key."
    }
  }
}

# The slot of the outputs of 00-bootstrap. Nothing writes it yet. Its writer must overwrite
# it as a SecureString with --key-id <this key>, as the role that applies this stack. Until
# then the value is a placeholder that stops the start of the core, so apply phase 2 after
# the outputs are written.
resource "aws_ssm_parameter" "bootstrap" {
  name   = "${local.parameter_path}/CLANKER_AWS_BOOTSTRAP"
  type   = "SecureString"
  key_id = aws_kms_key.settings.arn
  value  = "not written: put the outputs of 00-bootstrap here"

  lifecycle {
    ignore_changes = [value]
  }
}

resource "aws_ssm_parameter" "idp" {
  for_each = local.idp_setting_names

  name   = "${local.idp_parameter_path}/${each.key}"
  type   = "SecureString"
  key_id = aws_kms_key.settings.arn
  value  = local.idp_settings[each.key]
}
