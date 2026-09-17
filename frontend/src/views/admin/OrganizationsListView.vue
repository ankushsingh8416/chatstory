<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { organizationsService, type Organization } from '@/services/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { PageHeader, DataTable, SearchInput, ErrorState, type Column } from '@/components/shared'
import { toast } from 'vue-sonner'
import { Plus, Building2 } from 'lucide-vue-next'
import { getErrorMessage } from '@/lib/api-utils'
import { formatDate } from '@/lib/utils'

const { t } = useI18n()

const organizations = ref<Organization[]>([])
const isLoading = ref(false)
const error = ref(false)
const searchQuery = ref('')

const sortKey = ref('name')
const sortDirection = ref<'asc' | 'desc'>('asc')

const columns = computed<Column<Organization>[]>(() => [
  { key: 'name', label: t('admin.organizations.name'), sortable: true },
  { key: 'slug', label: t('admin.organizations.slug') },
  { key: 'members', label: t('admin.organizations.members'), sortable: true, sortKey: 'member_count' },
  { key: 'status', label: t('common.status') },
  { key: 'created', label: t('admin.organizations.created'), sortable: true, sortKey: 'created_at' },
])

const filteredOrganizations = computed(() => {
  if (!searchQuery.value.trim()) return organizations.value
  const q = searchQuery.value.trim().toLowerCase()
  return organizations.value.filter(o =>
    o.name.toLowerCase().includes(q) || (o.slug || '').toLowerCase().includes(q)
  )
})

async function fetchOrganizations() {
  isLoading.value = true
  error.value = false
  try {
    const response = await organizationsService.list()
    const data = (response.data as any).data || response.data
    organizations.value = data.organizations || []
  } catch (e) {
    error.value = true
    toast.error(getErrorMessage(e, t('common.failedLoad', { resource: t('resources.organizations') })))
  } finally {
    isLoading.value = false
  }
}

onMounted(() => fetchOrganizations())
</script>

<template>
  <div class="flex flex-col h-full bg-[#0a0a0b] light:bg-gray-50">
    <PageHeader
      :title="$t('admin.organizations.title')"
      :description="$t('admin.organizations.subtitle')"
      :icon="Building2"
      icon-gradient="bg-gradient-to-br from-violet-500 to-purple-600 shadow-violet-500/20"
      back-link="/"
    >
      <template #actions>
        <RouterLink to="/admin/organizations/new">
          <Button variant="outline" size="sm"><Plus class="h-4 w-4 mr-2" />{{ $t('admin.organizations.createOrganization') }}</Button>
        </RouterLink>
      </template>
    </PageHeader>

    <ErrorState
      v-if="error && !isLoading"
      :title="$t('admin.organizations.fetchErrorTitle')"
      :description="$t('admin.organizations.fetchErrorDescription')"
      :retry-label="$t('common.retry')"
      class="flex-1"
      @retry="fetchOrganizations"
    />

    <ScrollArea v-else class="flex-1">
      <div class="p-6">
        <Card>
          <CardHeader>
            <div class="flex items-center justify-between flex-wrap gap-4">
              <div>
                <CardTitle>{{ $t('admin.organizations.yourOrganizations') }}</CardTitle>
                <CardDescription>{{ $t('admin.organizations.yourOrganizationsDesc') }}</CardDescription>
              </div>
              <SearchInput v-model="searchQuery" :placeholder="$t('admin.organizations.searchOrganizations') + '...'" class="w-64" />
            </div>
          </CardHeader>
          <CardContent>
            <DataTable
              :items="filteredOrganizations"
              :columns="columns"
              :is-loading="isLoading"
              :empty-icon="Building2"
              :empty-title="searchQuery ? $t('admin.organizations.noMatchingOrganizations') : $t('admin.organizations.noOrganizationsYet')"
              :empty-description="searchQuery ? $t('admin.organizations.noMatchingOrganizationsDesc') : $t('admin.organizations.noOrganizationsYetDesc')"
              v-model:sort-key="sortKey"
              v-model:sort-direction="sortDirection"
              item-name="organizations"
            >
              <template #cell-name="{ item: org }">
                <RouterLink :to="`/admin/organizations/${org.id}`" class="flex items-center gap-2 font-medium text-inherit no-underline hover:opacity-80">
                  <span class="h-6 w-6 rounded border bg-muted/30 flex items-center justify-center overflow-hidden shrink-0">
                    <img v-if="org.logo_data_url" :src="org.logo_data_url" :alt="org.name" class="h-full w-full object-contain" />
                    <Building2 v-else class="h-3.5 w-3.5 text-muted-foreground/40" />
                  </span>
                  {{ org.name }}
                </RouterLink>
              </template>
              <template #cell-slug="{ item: org }"><span class="text-muted-foreground">{{ org.slug || '—' }}</span></template>
              <template #cell-members="{ item: org }"><span>{{ org.member_count ?? '—' }}</span></template>
              <template #cell-status="{ item: org }">
                <Badge :variant="org.suspended ? 'destructive' : 'default'">
                  {{ org.suspended ? $t('admin.organizations.suspended') : $t('common.active') }}
                </Badge>
              </template>
              <template #cell-created="{ item: org }"><span class="text-muted-foreground">{{ formatDate(org.created_at) }}</span></template>
              <template #empty-action>
                <RouterLink to="/admin/organizations/new">
                  <Button variant="outline" size="sm"><Plus class="h-4 w-4 mr-2" />{{ $t('admin.organizations.createOrganization') }}</Button>
                </RouterLink>
              </template>
            </DataTable>
          </CardContent>
        </Card>
      </div>
    </ScrollArea>
  </div>
</template>
