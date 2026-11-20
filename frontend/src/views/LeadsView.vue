<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import DataTable from '@/components/shared/DataTable.vue'
import type { Column } from '@/components/shared/types'
import { ErrorState } from '@/components/shared'
import { leadService, type Lead } from '@/services/api'
import { Download, RefreshCw, UserPlus } from 'lucide-vue-next'

const leads = ref<Lead[]>([])
const total = ref(0)
const currentPage = ref(1)
const pageSize = 20
const isLoading = ref(false)
const error = ref<string | null>(null)

const columns = computed<Column<Lead>[]>(() => [
  { key: 'name', label: 'Name' },
  { key: 'mobile', label: 'Mobile' },
  { key: 'email', label: 'Email' },
  { key: 'project', label: 'Project' },
  { key: 'source', label: 'Source' },
  { key: 'created_at', label: 'Captured At' },
])

async function fetchLeads() {
  isLoading.value = true
  error.value = null
  try {
    const res = await leadService.list(currentPage.value, pageSize)
    leads.value = res.data.data.leads
    total.value = res.data.data.total
  } catch {
    error.value = 'Failed to load leads'
  } finally {
    isLoading.value = false
  }
}

function handlePageChange(page: number) {
  currentPage.value = page
  fetchLeads()
}

function formatDate(dateStr: string): string {
  return new Date(dateStr).toLocaleString()
}

function downloadExport() {
  window.open(leadService.getExportUrl(), '_blank')
}

onMounted(fetchLeads)
</script>

<template>
  <div class="p-6 space-y-6">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-bold">Leads</h1>
        <p class="text-muted-foreground">Every lead captured by the chatbot — form submissions and free-text phone-number pickups.</p>
      </div>
      <div class="flex items-center gap-2">
        <Button variant="outline" size="sm" @click="fetchLeads">
          <RefreshCw class="h-4 w-4 mr-2" />
          Refresh
        </Button>
        <Button size="sm" @click="downloadExport">
          <Download class="h-4 w-4 mr-2" />
          Export CSV
        </Button>
      </div>
    </div>

    <ErrorState
      v-if="error"
      title="Could not load leads"
      :description="error"
      retry-label="Retry"
      @retry="fetchLeads"
    />

    <Card v-else>
      <CardContent class="pt-6">
        <DataTable
          :items="leads"
          :columns="columns"
          :is-loading="isLoading"
          :empty-icon="UserPlus"
          empty-title="No leads captured yet"
          server-pagination
          :current-page="currentPage"
          :total-items="total"
          :page-size="pageSize"
          item-name="leads"
          max-height="calc(100vh - 260px)"
          @page-change="handlePageChange"
        >
          <template #cell-name="{ item }">
            <span class="font-medium">{{ item.name || '-' }}</span>
          </template>
          <template #cell-mobile="{ item }">
            {{ item.mobile || '-' }}
          </template>
          <template #cell-email="{ item }">
            {{ item.email || '-' }}
          </template>
          <template #cell-project="{ item }">
            {{ item.project || '-' }}
          </template>
          <template #cell-source="{ item }">
            <Badge variant="secondary">{{ item.source }}</Badge>
          </template>
          <template #cell-created_at="{ item }">
            {{ formatDate(item.created_at) }}
          </template>
        </DataTable>
      </CardContent>
    </Card>
  </div>
</template>
