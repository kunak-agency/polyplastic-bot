output "cabinet_url" {
  description = "Кабинет."
  value       = local.cabinet_url
}

output "cabinet_urls" {
  description = "Все адреса кабинета и API."
  value = {
    web = [for d in local.web_domains : "https://${d}"]
    api = var.api_domain != "" ? "https://${var.api_domain}/api/health" : null
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

output "call_auth_password" {
  description = "Basic auth страницы звонка (логин korsik): terraform output -raw call_auth_password"
  value       = random_password.call_auth.result
  sensitive   = true
}
