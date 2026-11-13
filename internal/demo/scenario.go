// Package demo holds the subscription-free incident used by the simulator.
package demo

// MonitorBody is an Azure Monitor common alert schema payload for the
// example-api null dereference that the local fake runtime can explain.
func MonitorBody() []byte {
	return []byte(`{
	  "schemaId": "azureMonitorCommonAlertSchema",
	  "data": {
	    "essentials": {
	      "alertId": "/subscriptions/00000000-0000-0000-0000-000000000000/providers/Microsoft.AlertsManagement/alerts/example-api-orders",
	      "alertRule": "example-api failed requests",
	      "severity": "Sev2",
	      "signalType": "Log",
	      "monitorCondition": "Fired",
	      "monitoringService": "Log Alerts V2",
	      "alertTargetIDs": ["/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/example-rg/providers/Microsoft.Web/sites/example-api"],
	      "originAlertId": "example-api-orders",
	      "firedDateTime": "2026-11-13T14:02:01.123Z",
	      "description": "Failed requests spiked after the production slot swap.",
	      "essentialsVersion": "1.0",
	      "alertContextVersion": "1.0"
	    },
	    "alertContext": {
	      "properties": {
	        "exceptionType": "System.NullReferenceException",
	        "message": "Object reference not set to an instance of an object.",
	        "operation": "POST /v1/orders",
	        "topFrame": "OrderService.Place",
	        "stack": "at Example.Orders.OrderService.Place(OrderService.cs:88)\nat Example.Orders.OrdersController.Post(OrdersController.cs:41)",
	        "errorRate": "0.184"
	      },
	      "condition": {
	        "windowSize": "PT5M",
	        "allOf": [{
	          "searchQuery": "requests | where success == false",
	          "metricName": "Http5xx",
	          "operator": "GreaterThan",
	          "threshold": "10",
	          "metricValue": 42
	        }]
	      }
	    }
	  }
	}`)
}
