<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { knowledgeBasesService, type KnowledgeBase } from '@/services/api'
import { useOrganizationsStore } from '@/stores/organizations'
import { useAuthStore } from '@/stores/auth'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { PageHeader, DataTable, SearchInput, DeleteConfirmDialog, IconButton, ErrorState, type Column } from '@/components/shared'
import { toast } from 'vue-sonner'
import { Plus, Trash2, Pencil, BookOpen } from 'lucide-vue-next'
import { getErrorMessage } from '@/lib/api-utils'
import { formatDate } from '@/lib/utils'

const { t } = useI18n()

const organizationsStore = useOrganizationsStore()
const authStore = useAuthStore()

const knowledgeBases = ref<KnowledgeBase[]>([])
const isLoading = ref(false)
const isDeleting = ref(false)
const error = ref(false)

const canWrite = computed(() => authStore.hasPermission('knowledge_base', 'write'))
const canDelete = computed(() => authStore.hasPermission('knowledge_base', 'delete'))

const isDeleteDialogOpen = ref(false)
const kbToDelete = ref<KnowledgeBase | null>(null)

const sortKey = ref('name')
const sortDirection = ref<'asc' | 'desc'>('asc')

const searchQuery = ref('')

const columns = computed<Column<KnowledgeBase>[]>(() => [
  { key: 'name', label: t('knowledgeBase.name'), sortable: true },
  { key: 'dataConnection', label: t('knowledgeBase.dataConnection') },
  { key: 'documents', label: t('knowledgeBase.documents'), sortable: true, sortKey: 'document_count' },
  { key: 'status', label: t('common.status') },
  { key: 'created', label: t('knowledgeBase.created'), sortable: true, sortKey: 'created_at' },
  { key: 'actions', label: t('common.actions'), align: 'right' },
])

const filteredKnowledgeBases = computed(() => {
  if (!searchQuery.value.trim()) return knowledgeBases.value
  const q = searchQuery.value.trim().toLowerCase()
  return knowledgeBases.value.filter(kb => kb.name.toLowerCase().includes(q))
})

async function fetchKnowledgeBases() {
  isLoading.value = true
  error.value = false
  try {
    const response = await knowledgeBasesService.list()
    const data = (response.data as any).data || response.data
    knowledgeBases.value = data.knowledge_bases || []
  } catch (e) {
    error.value = true
    toast.error(getErrorMessage(e, t('common.failedLoad', { resource: t('resources.knowledgeBases') })))
  } finally {
    isLoading.value = false
  }
}

async function deleteKnowledgeBase() {
  if (!kbToDelete.value) return
  isDeleting.value = true
  try {
    await knowledgeBasesService.delete(kbToDelete.value.id)
    await fetchKnowledgeBases()
    toast.success(t('common.deletedSuccess', { resource: t('resources.KnowledgeBase') }))
    isDeleteDialogOpen.value = false
    kbToDelete.value = null
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedDelete', { resource: t('resources.knowledgeBase') })))
  } finally {
    isDeleting.value = false
  }
}

watch(() => organizationsStore.selectedOrgId, () => fetchKnowledgeBases())
onMounted(() => fetchKnowledgeBases())
</script>

<template>
  <div class="flex flex-col h-full bg-[#0a0a0b] light:bg-gray-50">
    <PageHeader :title="$t('knowledgeBase.title')" :description="$t('knowledgeBase.subtitle')" :icon="BookOpen" icon-gradient="bg-gradient-to-br from-amber-500 to-orange-600 shadow-amber-500/20" back-link="/chatbot">
      <template #actions>
        <RouterLink v-if="canWrite" to="/chatbot/knowledge-base/new">
          <Button variant="outline" size="sm"><Plus class="h-4 w-4 mr-2" />{{ $t('knowledgeBase.createKnowledgeBase') }}</Button>
        </RouterLink>
      </template>
    </PageHeader>

    <ErrorState
      v-if="error && !isLoading"
      :title="$t('knowledgeBase.fetchErrorTitle')"
      :description="$t('knowledgeBase.fetchErrorDescription')"
      :retry-label="$t('common.retry')"
      class="flex-1"
      @retry="fetchKnowledgeBases"
    />

    <ScrollArea v-else class="flex-1">
      <div class="p-6">
        <Card>
          <CardHeader>
            <div class="flex items-center justify-between flex-wrap gap-4">
              <div>
                <CardTitle>{{ $t('knowledgeBase.yourKnowledgeBases') }}</CardTitle>
                <CardDescription>{{ $t('knowledgeBase.yourKnowledgeBasesDesc') }}</CardDescription>
              </div>
              <SearchInput v-model="searchQuery" :placeholder="$t('knowledgeBase.searchKnowledgeBases') + '...'" class="w-64" />
            </div>
          </CardHeader>
          <CardContent>
            <DataTable :items="filteredKnowledgeBases" :columns="columns" :is-loading="isLoading" :empty-icon="BookOpen" :empty-title="searchQuery ? $t('knowledgeBase.noMatchingKnowledgeBases') : $t('knowledgeBase.noKnowledgeBasesYet')" :empty-description="searchQuery ? $t('knowledgeBase.noMatchingKnowledgeBasesDesc') : $t('knowledgeBase.noKnowledgeBasesYetDesc')" v-model:sort-key="sortKey" v-model:sort-direction="sortDirection" item-name="knowledgeBases">
              <template #cell-name="{ item: kb }">
                <RouterLink :to="`/chatbot/knowledge-base/${kb.id}`" class="font-medium text-inherit no-underline hover:opacity-80">{{ kb.name }}</RouterLink>
              </template>
              <template #cell-dataConnection="{ item: kb }"><span class="text-muted-foreground">{{ kb.data_connection_name || '—' }}</span></template>
              <template #cell-documents="{ item: kb }"><span>{{ kb.document_count }}</span></template>
              <template #cell-status="{ item: kb }">
                <Badge :variant="kb.is_active ? 'default' : 'secondary'">{{ kb.is_active ? $t('common.active') : $t('common.inactive') }}</Badge>
              </template>
              <template #cell-created="{ item: kb }"><span class="text-muted-foreground">{{ formatDate(kb.created_at) }}</span></template>
              <template #cell-actions="{ item: kb }">
                <div class="flex items-center justify-end gap-1">
                  <RouterLink :to="`/chatbot/knowledge-base/${kb.id}`">
                    <IconButton :icon="Pencil" :label="$t('common.edit')" class="h-8 w-8" />
                  </RouterLink>
                  <IconButton v-if="canDelete" :icon="Trash2" :label="$t('common.delete')" class="h-8 w-8 text-destructive" @click="kbToDelete = kb; isDeleteDialogOpen = true" />
                </div>
              </template>
              <template #empty-action>
                <RouterLink v-if="canWrite" to="/chatbot/knowledge-base/new">
                  <Button variant="outline" size="sm"><Plus class="h-4 w-4 mr-2" />{{ $t('knowledgeBase.createKnowledgeBase') }}</Button>
                </RouterLink>
              </template>
            </DataTable>
          </CardContent>
        </Card>
      </div>
    </ScrollArea>

    <DeleteConfirmDialog v-model:open="isDeleteDialogOpen" :title="$t('knowledgeBase.deleteKnowledgeBase')" :description="$t('knowledgeBase.deleteKnowledgeBaseWarning')" :item-name="kbToDelete?.name" :is-submitting="isDeleting" @confirm="deleteKnowledgeBase" />
  </div>
</template>
