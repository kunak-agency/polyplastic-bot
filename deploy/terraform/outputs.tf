output "cabinet_url" {
  description = "Кабинет. sslip.io работает сразу, домен кабинета — после A-записи на host."
  value       = "https://${var.fallback_domain}"
}

output "cabinet_urls" {
  description = "Все адреса кабинета и API."
  value = {
    web      = [for d in local.web_domains : "https://${d}"]
    api      = var.api_domain != "" ? "https://${var.api_domain}/api/health" : null
    fallback = "https://${var.fallback_domain}"
  }
}

output "basic_auth_user" {
  value = "jury"
}

output "basic_auth_password" {
  description = "terraform output -raw basic_auth_password"
  value       = random_password.basic_auth.result
  sensitive   = true
}
