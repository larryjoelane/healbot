// Container Apps deployment. Terraform roots:
//   deploy/terraform/app-service
//   deploy/terraform/container-apps
targetScope = 'resourceGroup'

@description('Region for the Container Apps environment.')
param location string = resourceGroup().location

@description('Name of the container app and the prefix for the action group.')
param botName string = 'healbot'

@description('Image built from the repo Dockerfile.')
param containerImage string

@secure()
@description('Shared secret sent by the action group. Also set as HEALBOT_WEBHOOK_TOKEN.')
param webhookToken string

@description('Full resource ID of a Log Analytics workspace. Empty disables the poller env value.')
param logAnalyticsWorkspaceId string = ''

@description('Grant Microsoft.Web/serverfarms/write so App Service scale-out can change plan capacity.')
param enablePlanScale bool = false

@description('Grant Microsoft.App/containerApps/write so container scale and traffic-weight rollback can patch the app.')
param enableContainerScale bool = false

var workspaceGuid = logAnalyticsWorkspaceId == '' ? '' : last(split(logAnalyticsWorkspaceId, '/'))

var readAndRestart = [
  'Microsoft.Web/sites/read'
  'Microsoft.Web/sites/restart/action'
  'Microsoft.Web/sites/slots/read'
  'Microsoft.Web/sites/slots/restart/action'
  'Microsoft.Web/sites/slots/slotsswap/action'
  'Microsoft.Web/sites/deployments/read'
  'Microsoft.Web/sites/config/read'
  'Microsoft.Web/serverfarms/read'
  'Microsoft.App/containerApps/read'
  'Microsoft.App/containerApps/revisions/read'
  'Microsoft.App/containerApps/revisions/restart/action'
  'Microsoft.App/containerApps/revisions/activate/action'
  'Microsoft.Insights/components/read'
  'Microsoft.OperationalInsights/workspaces/read'
  'Microsoft.OperationalInsights/workspaces/query/action'
]

var planScale = enablePlanScale ? [
  'Microsoft.Web/serverfarms/write'
] : []

var containerScale = enableContainerScale ? [
  'Microsoft.App/containerApps/write'
] : []

resource env 'Microsoft.App/managedEnvironments@2024-03-01' = {
  name: '${botName}-env'
  location: location
  properties: {}
}

resource bot 'Microsoft.App/containerApps@2024-03-01' = {
  name: botName
  location: location
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    managedEnvironmentId: env.id
    configuration: {
      activeRevisionsMode: 'Single'
      ingress: {
        external: true
        targetPort: 43117
        transport: 'auto'
        allowInsecure: false
      }
      secrets: [
        {
          name: 'webhook-token'
          value: webhookToken
        }
      ]
    }
    template: {
      containers: [
        {
          name: 'healbot'
          image: containerImage
          resources: {
            cpu: json('0.5')
            memory: '1Gi'
          }
          env: [
            { name: 'HEALBOT_MODE', value: 'azure' }
            { name: 'HEALBOT_DRY_RUN', value: 'true' }
            { name: 'HEALBOT_ADDR', value: ':43117' }
            { name: 'HEALBOT_SUBSCRIPTION_ID', value: subscription().subscriptionId }
            { name: 'HEALBOT_WORKSPACE_ID', value: workspaceGuid }
            { name: 'HEALBOT_AUDIT_PATH', value: '' }
            { name: 'HEALBOT_WEBHOOK_TOKEN', secretRef: 'webhook-token' }
          ]
          probes: [
            {
              type: 'Liveness'
              httpGet: {
                path: '/healthz'
                port: 43117
              }
              periodSeconds: 30
            }
            {
              type: 'Readiness'
              httpGet: {
                path: '/readyz'
                port: 43117
              }
              periodSeconds: 10
            }
          ]
        }
      ]
      scale: {
        minReplicas: 1
        maxReplicas: 2
      }
    }
  }
}

resource operator 'Microsoft.Authorization/roleDefinitions@2022-04-01' = {
  name: guid(resourceGroup().id, 'healbot-operator')
  properties: {
    roleName: 'Healbot Operator ${uniqueString(resourceGroup().id)}'
    description: 'Read diagnostics and run Healbot reversible actions. Delete and site write are denied.'
    type: 'CustomRole'
    permissions: [
      {
        actions: concat(readAndRestart, planScale, containerScale)
        notActions: [
          'Microsoft.Web/sites/delete'
          'Microsoft.Web/sites/write'
          'Microsoft.App/containerApps/delete'
        ]
        dataActions: []
        notDataActions: []
      }
    ]
    assignableScopes: [
      resourceGroup().id
    ]
  }
}

resource assignment 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(resourceGroup().id, bot.id, operator.id)
  properties: {
    roleDefinitionId: operator.id
    principalId: bot.identity.principalId
    principalType: 'ServicePrincipal'
  }
}

resource actionGroup 'Microsoft.Insights/actionGroups@2023-01-01' = {
  name: '${botName}-ag'
  location: 'global'
  properties: {
    groupShortName: 'healbot'
    enabled: true
    webhookReceivers: [
      {
        name: 'healbot'
        // Action groups cannot set a custom header. The token is in the query
        // string; prefer a private ingress or Front Door in front of this URL.
        serviceUri: 'https://${bot.properties.configuration.ingress.fqdn}/v1/ingest/monitor?token=${webhookToken}'
        useCommonAlertSchema: true
      }
    ]
  }
}

output containerAppFqdn string = bot.properties.configuration.ingress.fqdn
output principalId string = bot.identity.principalId
output actionGroupId string = actionGroup.id
output roleDefinitionId string = operator.id
