<script setup lang="ts">
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import {
  dataConnectionsService,
  type DataConnection,
  type DataConnectionType,
  type DataConnectionStatus,
  type DataConnectionRequest,
} from '@/services/api'
import { toast } from 'vue-sonner'
import { getErrorMessage } from '@/lib/api-utils'
import { useUnsavedChangesGuard } from '@/composables/useUnsavedChangesGuard'
import DetailPageLayout from '@/components/shared/DetailPageLayout.vue'
import MetadataPanel from '@/components/shared/MetadataPanel.vue'
import UnsavedChangesDialog from '@/components/shared/UnsavedChangesDialog.vue'
import { DeleteConfirmDialog } from '@/components/shared'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import {
  Database,
  Trash2,
  Save,
  Play,
  Loader2,
  CheckCircle2,
  XCircle,
} from 'lucide-vue-next'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const authStore = useAuthStore()

const connectionId = computed(() => route.params.id as string)
const isNew = computed(() => connectionId.value === 'new')
const connection = ref<DataConnection | null>(null)
const isLoading = ref(true)
const isNotFound = ref(false)
const isSaving = ref(false)
const isDeleting = ref(false)
const isTesting = ref(false)
const hasChanges = ref(false)
const deleteDialogOpen = ref(false)
const testResult = ref<{ status: DataConnectionStatus; message: string } | null>(null)

const { showLeaveDialog, confirmLeave, cancelLeave } = useUnsavedChangesGuard(hasChanges)

const canWrite = computed(() => authStore.hasPermission('data_connections', 'write'))
const canDelete = computed(() => authStore.hasPermission('data_connections', 'delete'))
const canTest = computed(() => authStore.hasPermission('data_connections', 'execute'))

const form = ref({
  name: '',
  type: 'postgres' as DataConnectionType,
  host: '',
  port: 5432,
  database: '',
  username: '',
  password: '',
  ssl_mode: 'require',
  qdrant_url: '',
  qdrant_api_key: '',
})

// The type is only choosable at creation time; once saved it's fixed since
// changing it would invalidate the rest of the form's fields.
const effectiveType = computed<DataConnectionType>(() => (isNew.value ? form.value.type : (connection.value?.type ?? 'postgres')))

function typeLabel(type: DataConnectionType): string {
  return type === 'postgres' ? t('dataConnections.typePostgres') : t('dataConnections.typeQdrant')
}

function statusVariant(status: DataConnectionStatus): 'secondary' | 'success' | 'destructive' {
  if (status === 'ok') return 'success'
  if (status === 'failed') return 'destructive'
  return 'secondary'
}

function statusLabel(status: DataConnectionStatus): string {
  if (status === 'ok') return t('dataConnections.statusOk')
  if (status === 'failed') return t('dataConnections.statusFailed')
  return t('dataConnections.statusUntested')
}

const breadcrumbs = computed(() => [
  { label: t('nav.settings'), href: '/settings' },
  { label: t('nav.dataConnections'), href: '/settings/data-connections' },
  { label: isNew.value ? t('dataConnections.newConnection') : (connection.value?.name || form.value.name || '') },
])

// There is no GET-by-id endpoint for data connections; the list is
// org-scoped and expected to stay small, so the detail view finds the
// record it needs from the full list.
async function loadConnection() {
  isLoading.value = true
  isNotFound.value = false
  try {
    const response = await dataConnectionsService.list()
    const data = (response.data as any).data || response.data
    const items: DataConnection[] = data.data_connections || []
    const found = items.find(c => c.id === connectionId.value)
    if (!found) {
      isNotFound.value = true
      return
    }
    connection.value = found
    syncForm()
    nextTick(() => { hasChanges.value = false })
  } catch {
    isNotFound.value = true
  } finally {
    isLoading.value = false
  }
}

function syncForm() {
  if (!connection.value) return
  form.value = {
    name: connection.value.name,
    type: connection.value.type,
    host: connection.value.host || '',
    port: connection.value.port || 5432,
    database: connection.value.database || '',
    username: connection.value.username || '',
    password: '',
    ssl_mode: connection.value.ssl_mode || 'require',
    qdrant_url: connection.value.qdrant_url || '',
    qdrant_api_key: '',
  }
  testResult.value = null
}

watch(form, () => {
  hasChanges.value = true
}, { deep: true })

function validate(): boolean {
  if (!form.value.name.trim()) {
    toast.error(t('dataConnections.nameRequired'))
    return false
  }
  if (effectiveType.value === 'postgres') {
    if (!form.value.host.trim()) {
      toast.error(t('dataConnections.hostRequired'))
      return false
    }
    if (!form.value.database.trim()) {
      toast.error(t('dataConnections.databaseRequired'))
      return false
    }
    if (!form.value.username.trim()) {
      toast.error(t('dataConnections.usernameRequired'))
      return false
    }
    if (isNew.value && !form.value.password) {
      toast.error(t('dataConnections.passwordRequired'))
      return false
    }
  } else {
    if (!form.value.qdrant_url.trim()) {
      toast.error(t('dataConnections.qdrantUrlRequired'))
      return false
    }
  }
  return true
}

function buildPayload(): DataConnectionRequest {
  const payload: DataConnectionRequest = {
    name: form.value.name.trim(),
    type: effectiveType.value,
  }
  if (effectiveType.value === 'postgres') {
    payload.host = form.value.host.trim()
    payload.port = form.value.port || 5432
    payload.database = form.value.database.trim()
    payload.username = form.value.username.trim()
    payload.ssl_mode = form.value.ssl_mode || 'require'
    // Only send the password if the user actually typed one — an empty
    // string means "leave the stored credential unchanged" per the API.
    if (form.value.password) payload.password = form.value.password
  } else {
    payload.qdrant_url = form.value.qdrant_url.trim()
    if (form.value.qdrant_api_key) payload.qdrant_api_key = form.value.qdrant_api_key
  }
  return payload
}

async function save() {
  if (!validate()) return
  isSaving.value = true
  try {
    const payload = buildPayload()
    if (isNew.value) {
      const response = await dataConnectionsService.create(payload)
      const created = (response.data as any).data || response.data
      hasChanges.value = false
      toast.success(t('common.createdSuccess', { resource: t('resources.DataConnection') }))
      router.replace(`/settings/data-connections/${created.id}`)
    } else {
      await dataConnectionsService.update(connection.value!.id, payload)
      toast.success(t('common.updatedSuccess', { resource: t('resources.DataConnection') }))
      await loadConnection()
    }
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedSave', { resource: t('resources.dataConnection') })))
  } finally {
    isSaving.value = false
  }
}

async function testConnection() {
  if (!connection.value) return
  isTesting.value = true
  testResult.value = null
  try {
    const response = await dataConnectionsService.test(connection.value.id)
    const result = (response.data as any).data || response.data
    testResult.value = result
    if (result.status === 'ok') {
      toast.success(t('dataConnections.connectionSuccessful'))
    } else {
      toast.error(result.message || t('dataConnections.statusFailed'))
    }
    // The test result is persisted server-side (status/last_tested_at/
    // last_test_error) — reflect that locally without a full reload so any
    // in-progress unsaved edits in the form aren't clobbered.
    connection.value = {
      ...connection.value,
      status: result.status,
      last_tested_at: new Date().toISOString(),
      last_test_error: result.status === 'failed' ? result.message : undefined,
    }
  } catch (e) {
    const message = getErrorMessage(e, t('dataConnections.statusFailed'))
    testResult.value = { status: 'failed', message }
    toast.error(message)
  } finally {
    isTesting.value = false
  }
}

async function deleteConnection() {
  if (!connection.value) return
  isDeleting.value = true
  try {
    await dataConnectionsService.delete(connection.value.id)
    toast.success(t('common.deletedSuccess', { resource: t('resources.DataConnection') }))
    hasChanges.value = false
    router.push('/settings/data-connections')
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedDelete', { resource: t('resources.dataConnection') })))
  } finally {
    isDeleting.value = false
    deleteDialogOpen.value = false
  }
}

onMounted(async () => {
  if (isNew.value) {
    isLoading.value = false
    hasChanges.value = false
  } else {
    await loadConnection()
  }
})
</script>

<template>
  <div class="h-full">
    <DetailPageLayout
      :title="isNew ? $t('dataConnections.newConnection') : (connection?.name || '')"
      :icon="Database"
      icon-gradient="bg-gradient-to-br from-cyan-500 to-blue-600 shadow-cyan-500/20"
      back-link="/settings/data-connections"
      :breadcrumbs="breadcrumbs"
      :is-loading="isLoading"
      :is-not-found="isNotFound"
      :not-found-title="$t('dataConnections.notFound')"
    >
      <template #actions>
        <div class="flex items-center gap-2">
          <Button v-if="canWrite && (hasChanges || isNew)" size="sm" @click="save" :disabled="isSaving">
            <Save class="h-4 w-4 mr-1" /> {{ isSaving ? $t('common.saving', 'Saving...') : isNew ? $t('common.create') : $t('common.save') }}
          </Button>
          <Button v-if="!isNew && canTest" variant="outline" size="sm" @click="testConnection" :disabled="isTesting">
            <Loader2 v-if="isTesting" class="h-4 w-4 animate-spin mr-1" />
            <Play v-else class="h-4 w-4 mr-1" />
            {{ isTesting ? $t('dataConnections.testing') : $t('dataConnections.testConnection') }}
          </Button>
          <Button v-if="canDelete && !isNew" variant="destructive" size="sm" @click="deleteDialogOpen = true">
            <Trash2 class="h-4 w-4 mr-1" /> {{ $t('common.delete') }}
          </Button>
        </div>
      </template>

      <Card>
        <CardHeader class="pb-3">
          <div class="flex items-center justify-between">
            <CardTitle class="text-sm font-medium">{{ $t('teams.details', 'Details') }}</CardTitle>
            <Badge v-if="!isNew && connection" :variant="statusVariant(connection.status)">
              {{ statusLabel(connection.status) }}
            </Badge>
          </div>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('dataConnections.name') }} <span class="text-destructive">*</span></Label>
            <Input v-model="form.name" :placeholder="$t('dataConnections.namePlaceholder')" :disabled="!canWrite" />
          </div>

          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('dataConnections.connectionType') }} <span class="text-destructive">*</span></Label>
            <RadioGroup v-if="isNew" v-model="form.type" class="flex flex-col gap-2">
              <div class="flex items-center space-x-2">
                <RadioGroupItem value="postgres" id="type-postgres" />
                <Label for="type-postgres" class="cursor-pointer font-normal">{{ $t('dataConnections.typePostgres') }}</Label>
              </div>
              <div class="flex items-center space-x-2">
                <RadioGroupItem value="qdrant" id="type-qdrant" />
                <Label for="type-qdrant" class="cursor-pointer font-normal">{{ $t('dataConnections.typeQdrant') }}</Label>
              </div>
            </RadioGroup>
            <template v-else>
              <div>
                <Badge variant="outline">{{ typeLabel(effectiveType) }}</Badge>
                <p class="text-xs text-muted-foreground mt-1">{{ $t('dataConnections.connectionTypeHint') }}</p>
              </div>
            </template>
          </div>

          <template v-if="effectiveType === 'postgres'">
            <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <div class="sm:col-span-2 space-y-1.5">
                <Label class="text-xs">{{ $t('dataConnections.host') }} <span class="text-destructive">*</span></Label>
                <Input v-model="form.host" :placeholder="$t('dataConnections.hostPlaceholder')" :disabled="!canWrite" />
              </div>
              <div class="space-y-1.5">
                <Label class="text-xs">{{ $t('dataConnections.port') }}</Label>
                <Input v-model.number="form.port" type="number" :disabled="!canWrite" />
              </div>
            </div>
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('dataConnections.database') }} <span class="text-destructive">*</span></Label>
              <Input v-model="form.database" :placeholder="$t('dataConnections.databasePlaceholder')" :disabled="!canWrite" />
            </div>
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('dataConnections.username') }} <span class="text-destructive">*</span></Label>
              <Input v-model="form.username" :disabled="!canWrite" />
            </div>
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('dataConnections.password') }} <span v-if="isNew" class="text-destructive">*</span></Label>
              <Input
                v-model="form.password"
                type="password"
                :placeholder="isNew ? '' : (connection?.has_password ? $t('dataConnections.passwordPlaceholderSet') : $t('dataConnections.passwordPlaceholderUnset'))"
                :disabled="!canWrite"
              />
            </div>
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('dataConnections.sslMode') }}</Label>
              <Select v-model="form.ssl_mode" :disabled="!canWrite">
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="require">require</SelectItem>
                  <SelectItem value="prefer">prefer</SelectItem>
                  <SelectItem value="disable">disable</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </template>

          <template v-else>
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('dataConnections.qdrantUrl') }} <span class="text-destructive">*</span></Label>
              <Input v-model="form.qdrant_url" :placeholder="$t('dataConnections.qdrantUrlPlaceholder')" :disabled="!canWrite" />
            </div>
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('dataConnections.qdrantApiKey') }}</Label>
              <Input
                v-model="form.qdrant_api_key"
                type="password"
                :placeholder="isNew ? '' : (connection?.has_qdrant_key ? $t('dataConnections.qdrantApiKeyPlaceholderSet') : $t('dataConnections.qdrantApiKeyPlaceholderUnset'))"
                :disabled="!canWrite"
              />
            </div>
          </template>

          <div v-if="!isNew && testResult" class="rounded-lg border p-3 space-y-2" :class="testResult.status === 'ok' ? 'border-emerald-800 bg-emerald-950/30 light:border-emerald-200 light:bg-emerald-50' : 'border-red-800 bg-red-950/30 light:border-red-200 light:bg-red-50'">
            <div v-if="testResult.status === 'ok'" class="flex items-center gap-2 text-emerald-400 light:text-emerald-600">
              <CheckCircle2 class="h-4 w-4" />
              <span class="text-sm font-medium">{{ $t('dataConnections.connectionSuccessful') }}</span>
            </div>
            <div v-else class="space-y-1.5">
              <div class="flex items-center gap-2 text-red-400 light:text-red-600">
                <XCircle class="h-4 w-4" />
                <span class="text-sm font-medium">{{ $t('dataConnections.statusFailed') }}</span>
              </div>
              <pre class="text-xs font-mono whitespace-pre-wrap break-all text-muted-foreground bg-black/20 light:bg-white rounded p-2 border border-border/40">{{ testResult.message }}</pre>
            </div>
          </div>

          <div v-else-if="!isNew && connection?.last_test_error" class="rounded-lg border border-red-800 bg-red-950/30 light:border-red-200 light:bg-red-50 p-3 space-y-1.5">
            <div class="flex items-center gap-2 text-red-400 light:text-red-600">
              <XCircle class="h-4 w-4" />
              <span class="text-sm font-medium">{{ $t('dataConnections.statusFailed') }}</span>
            </div>
            <pre class="text-xs font-mono whitespace-pre-wrap break-all text-muted-foreground bg-black/20 light:bg-white rounded p-2 border border-border/40">{{ connection.last_test_error }}</pre>
          </div>
        </CardContent>
      </Card>

      <template v-if="!isNew" #sidebar>
        <MetadataPanel
          :created-at="connection?.created_at"
          :updated-at="connection?.updated_at"
        />
      </template>
    </DetailPageLayout>

    <DeleteConfirmDialog
      v-model:open="deleteDialogOpen"
      :title="$t('dataConnections.deleteConnection')"
      :item-name="connection?.name"
      :is-submitting="isDeleting"
      @confirm="deleteConnection"
    />

    <UnsavedChangesDialog :open="showLeaveDialog" @stay="cancelLeave" @leave="confirmLeave" />
  </div>
</template>
