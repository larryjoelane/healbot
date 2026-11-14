output "id" {
  description = "Resource ID of the Linux web app."
  value       = azurerm_linux_web_app.bot.id
}

output "hostname" {
  description = "Default hostname, without a scheme."
  value       = azurerm_linux_web_app.bot.default_hostname
}

output "name" {
  description = "App Service name."
  value       = azurerm_linux_web_app.bot.name
}
