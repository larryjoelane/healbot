output "action_group_id" {
  description = "Action group that posts the common alert schema to Healbot. Attach existing alerts to this id."
  value       = azurerm_monitor_action_group.bot.id
}
