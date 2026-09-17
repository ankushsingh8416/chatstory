<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { dataConnectionsService, type DataConnection } from '@/services/api'
import { useOrganizationsStore } from '@/stores/organizations'
import { useAuthStore } from '@/stores/auth'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { PageHeader, DataTable, SearchInput, DeleteConfirmDialog, IconButton, ErrorState, type Column } from '@/components/shared'
import { toast } from 'vue-sonner'
import { Plus, Trash2, Pencil, Database } from 'lucide-vue-next'
import { getErrorMessage } from '@/lib/api-utils'
import { formatDate } from '@/lib/utils'

const { t } = useI18n()

const organizationsStore = useOrganizationsStore()
const authStore = useAuthStore()

const connections = ref<DataConnection[]>([])
const isLoading = ref(false)
const isDeleting = ref(false)
const error = ref(false)

const canWrite = computed(() => authStore.hasPermission('data_connections', 'write'))
const canDelete = computed(() => authStore.hasPermission('data_connections', 'delete'))

const isDeleteDialogOpen = ref(false)
const connectionToDelete = ref<DataConnection | null>(null)

const sortKey = ref('name')
const sortDirection = ref<'asc' | 'desc'>('asc')

const searchQuery = ref('')

const columns = computed<Column<DataConnection>[]>(() => [
  { key: 'name', label: t('dataConnections.name'), sortable: true },
  { key: 'type', label: t('dataConnections.type'), sortable: true },
  { key: 'status', label: t('dataConnections.status'), sortable: true },
  { key: 'lastTested', label: t('dataConnections.lastTested'), sortable: true, sortKey: 'last_tested_at' },
  { key: 'created', label: t('dataConnections.created'), sortable: true, sortKey: 'created_at' },
  { key: 'actions', label: t('common.actions'), align: 'right' },
])

const filteredConnections = computed(() => {
  if (!searchQuery.value.trim()) return connections.value
  const q = searchQuery.value.trim().toLowerCase()
  return connections.value.filter(c => c.name.toLowerCase().includes(q))
})

function typeLabel(type: DataConnection['type']): string {
  return type === 'postgres' ? t('dataConnections.typePostgres') : t('dataConnections.typeQdrant')
}

function statusVariant(status: DataConnection['status']): 'secondary' | 'success' | 'destructive' {
  if (status === 'ok') return 'success'
  if (status === 'failed') return 'destructive'
  return 'secondary'
}

function statusLabel(status: DataConnection['status']): string {
  if (status === 'ok') return t('dataConnections.statusOk')
  if (status === 'failed') return t('dataConnections.statusFailed')
  return t('dataConnections.statusUntested')
}

async function fetchConnections() {
  isLoading.value = true
  error.value = false
  try {
    const response = await dataConnectionsService.list()
    const data = (response.data as any).data || response.data
    connections.value = data.data_connections || []
  } catch (e) {
    error.value = true
    toast.error(getErrorMessage(e, t('common.failedLoad', { resource: t('resources.dataConnections') })))
  } finally {
    isLoading.value = false
  }
}

async function deleteConnection() {
  if (!connectionToDelete.value) return
  isDeleting.value = true
  try {
    await dataConnectionsService.delete(connectionToDelete.value.id)
    await fetchConnections()
    toast.success(t('common.deletedSuccess', { resource: t('resources.DataConnection') }))
    isDeleteDialogOpen.value = false
    connectionToDelete.value = null
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedDelete', { resource: t('resources.dataConnection') })))
  } finally {
    isDeleting.value = false
  }
}

watch(() => organizationsStore.selectedOrgId, () => fetchConnections())
onMounted(() => fetchConnections())
</script>

<template>
  <div class="flex flex-col h-full bg-[#0a0a0b] light:bg-gray-50">
    <PageHeader :title="$t('dataConnections.title')" :subtitle="$t('dataConnections.subtitle')" :icon="Database" icon-gradient="bg-gradient-to-br from-cyan-500 to-blue-600 shadow-cyan-500/20" back-link="/chatbot">
      <template #actions>
        <RouterLink v-if="canWrite" to="/chatbot/data-connections/new">
          <Button variant="outline" size="sm"><Plus class="h-4 w-4 mr-2" />{{ $t('dataConnections.addConnection') }}</Button>
        </RouterLink>
      </template>
    </PageHeader>

    <ErrorState
      v-if="error && !isLoading"
      :title="$t('dataConnections.fetchErrorTitle')"
      :description="$t('dataConnections.fetchErrorDescription')"
      :retry-label="$t('common.retry')"
      class="flex-1"
      @retry="fetchConnections"
    />

    <ScrollArea v-else class="flex-1">
      <div class="p-6">
        <div>
          <Card>
            <CardHeader>
              <div class="flex items-center justify-between flex-wrap gap-4">
                <div>
                  <CardTitle>{{ $t('dataConnections.yourConnections') }}</CardTitle>
                  <CardDescription>{{ $t('dataConnections.yourConnectionsDesc') }}</CardDescription>
                </div>
                <SearchInput v-model="searchQuery" :placeholder="$t('dataConnections.searchConnections') + '...'" class="w-64" />
              </div>
            </CardHeader>
            <CardContent>
              <DataTable :items="filteredConnections" :columns="columns" :is-loading="isLoading" :empty-icon="Database" :empty-title="searchQuery ? $t('dataConnections.noMatchingConnections') : $t('dataConnections.noConnectionsYet')" :empty-description="searchQuery ? $t('dataConnections.noMatchingConnectionsDesc') : $t('dataConnections.noConnectionsYetDesc')" v-model:sort-key="sortKey" v-model:sort-direction="sortDirection" item-name="dataConnections">
                <template #cell-name="{ item: connection }">
                  <RouterLink :to="`/chatbot/data-connections/${connection.id}`" class="font-medium text-inherit no-underline hover:opacity-80">{{ connection.name }}</RouterLink>
                </template>
                <template #cell-type="{ item: connection }">
                  <Badge variant="outline">{{ typeLabel(connection.type) }}</Badge>
                </template>
                <template #cell-status="{ item: connection }">
                  <Badge :variant="statusVariant(connection.status)">{{ statusLabel(connection.status) }}</Badge>
                </template>
                <template #cell-lastTested="{ item: connection }">
                  <span class="text-muted-foreground">{{ connection.last_tested_at ? formatDate(connection.last_tested_at) : $t('dataConnections.never') }}</span>
                </template>
                <template #cell-created="{ item: connection }"><span class="text-muted-foreground">{{ formatDate(connection.created_at) }}</span></template>
                <template #cell-actions="{ item: connection }">
                  <div class="flex items-center justify-end gap-1">
                    <RouterLink :to="`/chatbot/data-connections/${connection.id}`">
                      <IconButton :icon="Pencil" :label="$t('common.edit')" class="h-8 w-8" />
                    </RouterLink>
                    <IconButton v-if="canDelete" :icon="Trash2" :label="$t('common.delete')" class="h-8 w-8 text-destructive" @click="connectionToDelete = connection; isDeleteDialogOpen = true" />
                  </div>
                </template>
                <template #empty-action>
                  <RouterLink v-if="canWrite" to="/chatbot/data-connections/new">
                    <Button variant="outline" size="sm"><Plus class="h-4 w-4 mr-2" />{{ $t('dataConnections.addConnection') }}</Button>
                  </RouterLink>
                </template>
              </DataTable>
            </CardContent>
          </Card>
        </div>
      </div>
    </ScrollArea>

    <DeleteConfirmDialog v-model:open="isDeleteDialogOpen" :title="$t('dataConnections.deleteConnection')" :item-name="connectionToDelete?.name" :is-submitting="isDeleting" @confirm="deleteConnection" />
  </div>
</template>
