output "id" {
  description = "Resource ID of the Container App."
  value       = azurerm_container_app.bot.id
}

output "hostname" {
  description = "Ingress FQDN, without a scheme."
  value       = azurerm_container_app.bot.ingress[0].fqdn
}

output "name" {
  description = "Container App name."
  value       = azurerm_container_app.bot.name
}
