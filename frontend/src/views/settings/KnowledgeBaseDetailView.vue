<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  knowledgeBasesService,
  dataConnectionsService,
  organizationService,
  chatbotService,
  type KnowledgeBase,
  type KBDocument,
  type KBDocumentStatus,
  type DataConnection,
  type DataConnectionType,
  type DataConnectionRequest,
  type DataConnectionTable,
} from '@/services/api'
import { toast } from 'vue-sonner'
import { getErrorMessage } from '@/lib/api-utils'
import { formatDate } from '@/lib/utils'
import { useUnsavedChangesGuard } from '@/composables/useUnsavedChangesGuard'
import { DetailPageLayout, MetadataPanel, DeleteConfirmDialog, DataTable, IconButton, type Column } from '@/components/shared'
import UnsavedChangesDialog from '@/components/shared/UnsavedChangesDialog.vue'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import {
  BookOpen,
  Trash2,
  Save,
  RotateCw,
  Loader2,
  AlertTriangle,
  FileText,
  Upload,
  Plus,
  MessageSquare,
} from 'lucide-vue-next'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const kbId = computed(() => route.params.id as string)
const isNew = computed(() => kbId.value === 'new')
const kb = ref<KnowledgeBase | null>(null)
const isLoading = ref(true)
const isNotFound = ref(false)
const isSaving = ref(false)
const isDeleting = ref(false)
const hasChanges = ref(false)
const deleteDialogOpen = ref(false)

const dataConnections = ref<DataConnection[]>([])
const hasEmbeddingsKey = ref(false)

const { showLeaveDialog, confirmLeave, cancelLeave } = useUnsavedChangesGuard(hasChanges)

const form = ref({
  name: '',
  description: '',
  data_connection_id: '',
  collection_name: '',
  content_column: '',
  embedding_column: '',
  embedding_dims: 1536,
  chunk_size: 1000,
  chunk_overlap: 150,
  is_active: true,
})

// --- Point at an existing (already-populated) vector table instead of
// ingesting new documents through this app — for orgs that already run
// their own embedding pipeline elsewhere and just want the chatbot to
// search it. Schema (table/column names) varies per org, so it's
// discovered from their own credentials rather than hand-typed.
const useExistingTable = ref(false)
const existingTables = ref<DataConnectionTable[]>([])
const isLoadingTables = ref(false)
const tablesLoadError = ref('')
const selectedTableName = ref('')
const selectedEmbeddingColumn = ref('')

const selectedConnection = computed(() => dataConnections.value.find(dc => dc.id === form.value.data_connection_id))
// A KB whose column names don't match this app's own ingestion shape is
// reading an org's externally-managed table — uploading a document through
// this app would try to INSERT into columns that don't exist there.
const isExternalTable = computed(() => !!kb.value && (kb.value.content_column !== 'content' || kb.value.embedding_column !== 'embedding'))
const selectedTable = computed(() => existingTables.value.find(t => t.name === selectedTableName.value))
const textColumnsOfSelectedTable = computed(() => (selectedTable.value?.columns || []).filter(c => !c.is_vector))
const vectorColumnsOfSelectedTable = computed(() => (selectedTable.value?.columns || []).filter(c => c.is_vector))

async function browseExistingTables() {
  if (!form.value.data_connection_id) {
    toast.error(t('knowledgeBase.selectDataConnectionFirst', 'Select a data connection first'))
    return
  }
  isLoadingTables.value = true
  tablesLoadError.value = ''
  try {
    const response = await dataConnectionsService.listTables(form.value.data_connection_id)
    const data = (response.data as any).data || response.data
    existingTables.value = data.tables || []
  } catch (e) {
    tablesLoadError.value = getErrorMessage(e, t('knowledgeBase.listTablesFailed', 'Could not read tables from this connection'))
  } finally {
    isLoadingTables.value = false
  }
}

watch(selectedTableName, () => {
  form.value.collection_name = selectedTableName.value
  selectedEmbeddingColumn.value = vectorColumnsOfSelectedTable.value[0]?.name || ''
  form.value.content_column = ''
})

watch(selectedEmbeddingColumn, () => {
  form.value.embedding_column = selectedEmbeddingColumn.value
  const col = vectorColumnsOfSelectedTable.value.find(c => c.name === selectedEmbeddingColumn.value)
  if (col?.vector_dims) form.value.embedding_dims = col.vector_dims
})

watch(useExistingTable, (on) => {
  if (on) {
    form.value.collection_name = ''
    form.value.content_column = ''
    form.value.embedding_column = ''
    selectedTableName.value = ''
    selectedEmbeddingColumn.value = ''
    if (form.value.data_connection_id) browseExistingTables()
  }
})

let collectionNameEdited = false
let settingCollectionNameProgrammatically = false

function slugify(name: string): string {
  let s = name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '')
    .replace(/_{2,}/g, '_')
  if (!s) return ''
  if (!/^[a-z_]/.test(s)) s = '_' + s
  return s.slice(0, 63)
}

// Auto-suggest the collection name from the KB name until the user edits
// collection_name directly — tracked via a reactive watcher (rather than an
// @input handler on the Input component) since it only emits update:modelValue.
watch(() => form.value.name, (name) => {
  if (isNew.value && !collectionNameEdited) {
    settingCollectionNameProgrammatically = true
    form.value.collection_name = slugify(name)
  }
})
watch(() => form.value.collection_name, () => {
  if (settingCollectionNameProgrammatically) {
    settingCollectionNameProgrammatically = false
    return
  }
  collectionNameEdited = true
})

watch(form, () => { hasChanges.value = true }, { deep: true })

const breadcrumbs = computed(() => [
  { label: t('nav.knowledgeBase'), href: '/chatbot/knowledge-base' },
  { label: isNew.value ? t('knowledgeBase.newKnowledgeBase') : (kb.value?.name || '') },
])

const collectionNamePattern = /^[a-zA-Z_][a-zA-Z0-9_]{0,62}$/

async function loadDataConnections() {
  try {
    const response = await dataConnectionsService.list()
    const data = (response.data as any).data || response.data
    dataConnections.value = data.data_connections || []
  } catch {
    // Non-fatal — the select will just be empty; the create form's empty
    // state below handles "no connections yet" explicitly.
  }
}

// --- Inline "Add Data Connection" — lets a user go from nothing to a
// working knowledge base without leaving this page (previously required a
// separate Data Connections page just to create the credential row first).
const isAddConnectionDialogOpen = ref(false)
const isCreatingConnection = ref(false)
const connForm = ref({
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

function openAddConnectionDialog() {
  connForm.value = { name: '', type: 'postgres', host: '', port: 5432, database: '', username: '', password: '', ssl_mode: 'require', qdrant_url: '', qdrant_api_key: '' }
  isAddConnectionDialogOpen.value = true
}

function validateConnForm(): boolean {
  if (!connForm.value.name.trim()) {
    toast.error(t('dataConnections.nameRequired'))
    return false
  }
  if (connForm.value.type === 'postgres') {
    if (!connForm.value.host.trim() || !connForm.value.database.trim() || !connForm.value.username.trim() || !connForm.value.password) {
      toast.error(t('dataConnections.allFieldsRequired', 'Host, database, username, and password are required'))
      return false
    }
  } else if (!connForm.value.qdrant_url.trim()) {
    toast.error(t('dataConnections.qdrantUrlRequired'))
    return false
  }
  return true
}

async function createConnectionInline() {
  if (!validateConnForm()) return
  isCreatingConnection.value = true
  try {
    const payload: DataConnectionRequest = { name: connForm.value.name.trim(), type: connForm.value.type }
    if (connForm.value.type === 'postgres') {
      payload.host = connForm.value.host.trim()
      payload.port = connForm.value.port || 5432
      payload.database = connForm.value.database.trim()
      payload.username = connForm.value.username.trim()
      payload.ssl_mode = connForm.value.ssl_mode || 'require'
      payload.password = connForm.value.password
    } else {
      payload.qdrant_url = connForm.value.qdrant_url.trim()
      if (connForm.value.qdrant_api_key) payload.qdrant_api_key = connForm.value.qdrant_api_key
    }
    const response = await dataConnectionsService.create(payload)
    const created = (response.data as any).data || response.data
    dataConnections.value.push(created)
    form.value.data_connection_id = created.id
    toast.success(t('common.createdSuccess', { resource: t('resources.DataConnection') }))
    isAddConnectionDialogOpen.value = false
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedSave', { resource: t('resources.dataConnection') })))
  } finally {
    isCreatingConnection.value = false
  }
}

async function loadEmbeddingsKeyStatus() {
  try {
    const response = await organizationService.getSettings()
    const data = (response.data as any).data || response.data
    hasEmbeddingsKey.value = !!data?.settings?.has_openai_embeddings_key
  } catch {
    hasEmbeddingsKey.value = false
  }
}

async function loadKB() {
  isLoading.value = true
  isNotFound.value = false
  try {
    const response = await knowledgeBasesService.list()
    const data = (response.data as any).data || response.data
    const items: KnowledgeBase[] = data.knowledge_bases || []
    const found = items.find(k => k.id === kbId.value)
    if (!found) {
      isNotFound.value = true
      return
    }
    kb.value = found
    form.value = {
      name: found.name,
      description: found.description || '',
      data_connection_id: found.data_connection_id,
      collection_name: found.collection_name,
      content_column: found.content_column || 'content',
      embedding_column: found.embedding_column || 'embedding',
      embedding_dims: found.embedding_dims,
      chunk_size: found.chunk_size,
      chunk_overlap: found.chunk_overlap,
      is_active: found.is_active,
    }
    nextTick(() => { hasChanges.value = false })
    await loadDocuments()
  } catch {
    isNotFound.value = true
  } finally {
    isLoading.value = false
  }
}

function validate(): boolean {
  if (!form.value.name.trim()) {
    toast.error(t('knowledgeBase.nameRequired'))
    return false
  }
  if (isNew.value) {
    if (!form.value.data_connection_id) {
      toast.error(t('knowledgeBase.dataConnectionRequired'))
      return false
    }
    if (useExistingTable.value) {
      if (!selectedTableName.value) {
        toast.error(t('knowledgeBase.selectTableRequired', 'Select a table'))
        return false
      }
      if (!form.value.content_column) {
        toast.error(t('knowledgeBase.selectContentColumnRequired', 'Select which column holds the text content'))
        return false
      }
      if (!form.value.embedding_column) {
        toast.error(t('knowledgeBase.selectEmbeddingColumnRequired', 'Select which column holds the embedding vector'))
        return false
      }
    } else if (!collectionNamePattern.test(form.value.collection_name)) {
      toast.error(t('knowledgeBase.collectionNameInvalid'))
      return false
    }
  }
  return true
}

async function save() {
  if (!validate()) return
  isSaving.value = true
  try {
    if (isNew.value) {
      const response = await knowledgeBasesService.create({
        name: form.value.name.trim(),
        description: form.value.description.trim(),
        data_connection_id: form.value.data_connection_id,
        collection_name: form.value.collection_name,
        content_column: useExistingTable.value ? form.value.content_column : undefined,
        embedding_column: useExistingTable.value ? form.value.embedding_column : undefined,
        embedding_dims: form.value.embedding_dims,
        chunk_size: form.value.chunk_size,
        chunk_overlap: form.value.chunk_overlap,
      })
      const created = (response.data as any).data || response.data
      hasChanges.value = false
      toast.success(t('common.createdSuccess', { resource: t('resources.KnowledgeBase') }))
      router.replace(`/chatbot/knowledge-base/${created.id}`)
    } else {
      await knowledgeBasesService.update(kb.value!.id, {
        name: form.value.name.trim(),
        description: form.value.description.trim(),
        chunk_size: form.value.chunk_size,
        chunk_overlap: form.value.chunk_overlap,
        is_active: form.value.is_active,
      })
      toast.success(t('common.updatedSuccess', { resource: t('resources.KnowledgeBase') }))
      await loadKB()
    }
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedSave', { resource: t('resources.knowledgeBase') })))
  } finally {
    isSaving.value = false
  }
}

async function deleteKB() {
  if (!kb.value) return
  isDeleting.value = true
  try {
    await knowledgeBasesService.delete(kb.value.id)
    toast.success(t('common.deletedSuccess', { resource: t('resources.KnowledgeBase') }))
    hasChanges.value = false
    router.push('/chatbot/knowledge-base')
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedDelete', { resource: t('resources.knowledgeBase') })))
  } finally {
    isDeleting.value = false
    deleteDialogOpen.value = false
  }
}

// --- Documents ---
const documents = ref<KBDocument[]>([])
const isDocumentsLoading = ref(false)
let pollTimer: ReturnType<typeof setInterval> | null = null

const documentColumns = computed<Column<KBDocument>[]>(() => [
  { key: 'name', label: t('common.name') },
  { key: 'source', label: t('knowledgeBase.source') },
  { key: 'status', label: t('common.status') },
  { key: 'chunks', label: t('knowledgeBase.chunks') },
  { key: 'created', label: t('knowledgeBase.created') },
  { key: 'actions', label: t('common.actions'), align: 'right' },
])

function statusVariant(status: KBDocumentStatus): 'secondary' | 'default' | 'success' | 'destructive' {
  if (status === 'completed') return 'success'
  if (status === 'failed') return 'destructive'
  if (status === 'processing') return 'default'
  return 'secondary'
}

function statusLabel(status: KBDocumentStatus): string {
  return t(`knowledgeBase.status_${status}`)
}

async function loadDocuments() {
  if (!kb.value) return
  isDocumentsLoading.value = true
  try {
    const response = await knowledgeBasesService.listDocuments(kb.value.id)
    const data = (response.data as any).data || response.data
    documents.value = data.documents || []
    manageDocumentPolling()
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedLoad', { resource: t('resources.documents') })))
  } finally {
    isDocumentsLoading.value = false
  }
}

function manageDocumentPolling() {
  const hasInFlight = documents.value.some(d => d.status === 'pending' || d.status === 'processing')
  if (hasInFlight && !pollTimer) {
    pollTimer = setInterval(async () => {
      if (!kb.value) return
      try {
        const response = await knowledgeBasesService.listDocuments(kb.value.id)
        const data = (response.data as any).data || response.data
        documents.value = data.documents || []
      } catch {
        // Silent — next tick retries; a transient poll failure shouldn't spam toasts.
      }
      const stillInFlight = documents.value.some(d => d.status === 'pending' || d.status === 'processing')
      if (!stillInFlight && pollTimer) {
        clearInterval(pollTimer)
        pollTimer = null
      }
    }, 3000)
  } else if (!hasInFlight && pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})

// Add document — paste text or upload a file
const addMode = ref<'text' | 'upload'>('text')
const isAddingDocument = ref(false)
const pasteForm = ref({ name: '', text: '' })
const uploadForm = ref<{ name: string; file: File | null }>({ name: '', file: null })
const fileInput = ref<HTMLInputElement | null>(null)

const MAX_DOCUMENT_BYTES = 2 * 1024 * 1024

function handleFileChange(e: Event) {
  const target = e.target as HTMLInputElement
  const file = target.files?.[0] || null
  if (file) {
    const lower = file.name.toLowerCase()
    if (!lower.endsWith('.txt') && !lower.endsWith('.md')) {
      toast.error(t('knowledgeBase.unsupportedFileType'))
      target.value = ''
      uploadForm.value.file = null
      return
    }
    if (file.size > MAX_DOCUMENT_BYTES) {
      toast.error(t('knowledgeBase.fileTooLarge'))
      target.value = ''
      uploadForm.value.file = null
      return
    }
  }
  uploadForm.value.file = file
}

async function addTextDocument() {
  if (!kb.value) return
  if (!pasteForm.value.name.trim() || !pasteForm.value.text.trim()) {
    toast.error(t('knowledgeBase.nameAndTextRequired'))
    return
  }
  if (pasteForm.value.text.length > MAX_DOCUMENT_BYTES) {
    toast.error(t('knowledgeBase.fileTooLarge'))
    return
  }
  isAddingDocument.value = true
  try {
    await knowledgeBasesService.createTextDocument(kb.value.id, {
      name: pasteForm.value.name.trim(),
      text: pasteForm.value.text,
    })
    toast.success(t('knowledgeBase.documentAdded'))
    pasteForm.value = { name: '', text: '' }
    await loadDocuments()
  } catch (e) {
    toast.error(getErrorMessage(e, t('knowledgeBase.addDocumentFailed')))
  } finally {
    isAddingDocument.value = false
  }
}

async function addUploadDocument() {
  if (!kb.value || !uploadForm.value.file) {
    toast.error(t('knowledgeBase.fileRequired'))
    return
  }
  isAddingDocument.value = true
  try {
    await knowledgeBasesService.uploadDocument(kb.value.id, uploadForm.value.file, uploadForm.value.name.trim() || undefined)
    toast.success(t('knowledgeBase.documentAdded'))
    uploadForm.value = { name: '', file: null }
    if (fileInput.value) fileInput.value.value = ''
    await loadDocuments()
  } catch (e) {
    toast.error(getErrorMessage(e, t('knowledgeBase.addDocumentFailed')))
  } finally {
    isAddingDocument.value = false
  }
}

const retryingId = ref<string | null>(null)
async function retryDocument(doc: KBDocument) {
  if (!kb.value) return
  retryingId.value = doc.id
  try {
    await knowledgeBasesService.retryDocument(kb.value.id, doc.id)
    toast.success(t('knowledgeBase.retryStarted'))
    await loadDocuments()
  } catch (e) {
    toast.error(getErrorMessage(e, t('knowledgeBase.retryFailed')))
  } finally {
    retryingId.value = null
  }
}

const documentToDelete = ref<KBDocument | null>(null)
const deleteDocumentDialogOpen = ref(false)
const isDeletingDocument = ref(false)
function openDeleteDocumentDialog(doc: KBDocument) {
  documentToDelete.value = doc
  deleteDocumentDialogOpen.value = true
}
async function confirmDeleteDocument() {
  if (!kb.value || !documentToDelete.value) return
  isDeletingDocument.value = true
  try {
    await knowledgeBasesService.deleteDocument(kb.value.id, documentToDelete.value.id)
    toast.success(t('common.deletedSuccess', { resource: t('resources.document') }))
    deleteDocumentDialogOpen.value = false
    documentToDelete.value = null
    await loadDocuments()
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedDelete', { resource: t('resources.document') })))
  } finally {
    isDeletingDocument.value = false
  }
}

// --- Chatbot usage — links this knowledge base into the chatbot's AI
// response pipeline via an AIContext record (context_type=knowledge_base),
// without the user needing to separately visit AI Contexts to wire it up.
const linkedContextId = ref<string | null>(null)
const chatbotEnabled = ref(false)
const chatbotTopK = ref(3)
const chatbotThreshold = ref(0)
const isSavingChatbotUsage = ref(false)

async function loadLinkedAIContext() {
  if (!kb.value) return
  try {
    const response = await chatbotService.listAIContexts({ limit: 200 })
    const data = (response.data as any).data || response.data
    const contexts: any[] = data.contexts || []
    const linked = contexts.find(c => c.context_type === 'knowledge_base' && c.api_config?.knowledge_base_id === kb.value!.id)
    if (linked) {
      linkedContextId.value = linked.id
      chatbotEnabled.value = !!linked.is_enabled
      chatbotTopK.value = linked.api_config?.top_k ?? 3
      chatbotThreshold.value = linked.api_config?.similarity_threshold ?? 0
    } else {
      linkedContextId.value = null
      chatbotEnabled.value = false
    }
  } catch {
    // Non-fatal — the toggle just starts unlinked; saving still creates it fresh.
  }
}

async function saveChatbotUsage() {
  if (!kb.value) return
  isSavingChatbotUsage.value = true
  try {
    const payload = {
      name: `${kb.value.name} (Knowledge Base)`,
      context_type: 'knowledge_base',
      trigger_keywords: [],
      static_content: '',
      api_config: {
        knowledge_base_id: kb.value.id,
        top_k: chatbotTopK.value,
        similarity_threshold: chatbotThreshold.value,
      },
      priority: 10,
      enabled: chatbotEnabled.value,
    }
    if (linkedContextId.value) {
      await chatbotService.updateAIContext(linkedContextId.value, payload)
    } else {
      const response = await chatbotService.createAIContext(payload)
      const created = (response.data as any).data || response.data
      linkedContextId.value = created.id
    }
    toast.success(t('knowledgeBase.chatbotUsageSaved', 'Chatbot usage updated'))
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedSave', { resource: t('resources.AIContext', 'AI Context') })))
  } finally {
    isSavingChatbotUsage.value = false
  }
}

onMounted(async () => {
  await loadDataConnections()
  await loadEmbeddingsKeyStatus()
  if (isNew.value) {
    isLoading.value = false
    hasChanges.value = false
  } else {
    await loadKB()
    await loadLinkedAIContext()
  }
})
</script>

<template>
  <div class="h-full">
    <DetailPageLayout
      :title="isNew ? $t('knowledgeBase.newKnowledgeBase') : (kb?.name || '')"
      :icon="BookOpen"
      icon-gradient="bg-gradient-to-br from-amber-500 to-orange-600 shadow-amber-500/20"
      back-link="/chatbot/knowledge-base"
      :breadcrumbs="breadcrumbs"
      :is-loading="isLoading"
      :is-not-found="isNotFound"
      :not-found-title="$t('knowledgeBase.notFound')"
    >
      <template #actions>
        <div class="flex items-center gap-2">
          <Button v-if="isNew || hasChanges" size="sm" :disabled="isSaving" @click="save">
            <Save class="h-4 w-4 mr-1" /> {{ isSaving ? $t('common.saving', 'Saving...') : isNew ? $t('common.create') : $t('common.save') }}
          </Button>
          <Button v-if="!isNew" variant="destructive" size="sm" @click="deleteDialogOpen = true">
            <Trash2 class="h-4 w-4 mr-1" /> {{ $t('common.delete') }}
          </Button>
        </div>
      </template>

      <!-- Create mode: no data connections yet -->
      <Card v-if="isNew && dataConnections.length === 0">
        <CardContent class="py-8 text-center space-y-3">
          <AlertTriangle class="h-8 w-8 mx-auto text-amber-500" />
          <p class="text-sm text-muted-foreground">{{ $t('knowledgeBase.noDataConnectionsYet') }}</p>
          <Button variant="outline" size="sm" @click="openAddConnectionDialog">
            <Plus class="h-4 w-4 mr-1" />{{ $t('knowledgeBase.createDataConnectionFirst') }}
          </Button>
        </CardContent>
      </Card>

      <template v-else>
        <Card>
          <CardHeader class="pb-3">
            <div class="flex items-center justify-between">
              <CardTitle class="text-sm font-medium">{{ $t('teams.details', 'Details') }}</CardTitle>
              <div v-if="!isNew" class="flex items-center gap-2">
                <Label class="text-xs text-muted-foreground">{{ $t('common.active') }}</Label>
                <Switch v-model:checked="form.is_active" />
              </div>
            </div>
          </CardHeader>
          <CardContent class="space-y-4">
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('knowledgeBase.name') }} <span class="text-destructive">*</span></Label>
              <Input v-model="form.name" :placeholder="$t('knowledgeBase.namePlaceholder')" />
            </div>
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('knowledgeBase.description') }}</Label>
              <Textarea v-model="form.description" :rows="2" :placeholder="$t('knowledgeBase.descriptionPlaceholder')" />
            </div>

            <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div class="space-y-1.5">
                <Label class="text-xs">{{ $t('knowledgeBase.dataConnection') }} <span v-if="isNew" class="text-destructive">*</span></Label>
                <div v-if="isNew" class="flex items-center gap-2">
                  <Select v-model="form.data_connection_id" class="flex-1">
                    <SelectTrigger>
                      <SelectValue :placeholder="$t('knowledgeBase.selectDataConnection')" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem v-for="dc in dataConnections" :key="dc.id" :value="dc.id">{{ dc.name }} ({{ dc.type }})</SelectItem>
                    </SelectContent>
                  </Select>
                  <Button type="button" variant="outline" size="sm" @click="openAddConnectionDialog">
                    <Plus class="h-4 w-4" />
                  </Button>
                </div>
                <Input v-else :model-value="kb?.data_connection_name || ''" readonly disabled class="text-muted-foreground" />
              </div>
              <div v-if="!isNew || !useExistingTable" class="space-y-1.5">
                <Label class="text-xs">{{ $t('knowledgeBase.collectionName') }} <span v-if="isNew" class="text-destructive">*</span></Label>
                <Input
                  v-if="isNew"
                  v-model="form.collection_name"
                  :placeholder="$t('knowledgeBase.collectionNamePlaceholder')"
                />
                <Input v-else :model-value="kb?.collection_name || ''" readonly disabled class="text-muted-foreground" />
              </div>
            </div>

            <!-- Point at an existing, already-populated vector table -->
            <div v-if="isNew && selectedConnection?.type === 'postgres'" class="rounded-lg border border-border/60 p-3 space-y-3">
              <div class="flex items-center gap-2">
                <Switch v-model:checked="useExistingTable" />
                <Label class="text-xs">{{ $t('knowledgeBase.useExistingTable', "I already have a table with embeddings — don't ingest new documents") }}</Label>
              </div>

              <div v-if="useExistingTable" class="space-y-3">
                <div v-if="tablesLoadError" class="text-xs text-destructive">{{ tablesLoadError }}</div>
                <div class="flex items-center gap-2">
                  <Select v-model="selectedTableName" class="flex-1">
                    <SelectTrigger>
                      <SelectValue :placeholder="isLoadingTables ? $t('common.loading', 'Loading...') : $t('knowledgeBase.selectTable', 'Select a table')" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem v-for="t2 in existingTables" :key="t2.name" :value="t2.name">
                        {{ t2.name }}<span v-if="t2.has_vector_column"> ✓ {{ $t('knowledgeBase.hasVectorColumn', 'has vector column') }}</span>
                      </SelectItem>
                    </SelectContent>
                  </Select>
                  <Button type="button" variant="outline" size="sm" :disabled="isLoadingTables" @click="browseExistingTables">
                    <Loader2 v-if="isLoadingTables" class="h-4 w-4 animate-spin" />
                    <RotateCw v-else class="h-4 w-4" />
                  </Button>
                </div>

                <div v-if="selectedTable" class="grid grid-cols-2 gap-3">
                  <div class="space-y-1.5">
                    <Label class="text-xs">{{ $t('knowledgeBase.embeddingColumn', 'Embedding column') }} <span class="text-destructive">*</span></Label>
                    <Select v-model="selectedEmbeddingColumn">
                      <SelectTrigger><SelectValue :placeholder="$t('knowledgeBase.selectColumn', 'Select column')" /></SelectTrigger>
                      <SelectContent>
                        <SelectItem v-for="c in vectorColumnsOfSelectedTable" :key="c.name" :value="c.name">{{ c.name }} (vector{{ c.vector_dims ? `[${c.vector_dims}]` : '' }})</SelectItem>
                      </SelectContent>
                    </Select>
                    <p v-if="vectorColumnsOfSelectedTable.length === 0" class="text-xs text-destructive">{{ $t('knowledgeBase.noVectorColumn', 'This table has no vector column') }}</p>
                  </div>
                  <div class="space-y-1.5">
                    <Label class="text-xs">{{ $t('knowledgeBase.contentColumn', 'Content column') }} <span class="text-destructive">*</span></Label>
                    <Select v-model="form.content_column">
                      <SelectTrigger><SelectValue :placeholder="$t('knowledgeBase.selectColumn', 'Select column')" /></SelectTrigger>
                      <SelectContent>
                        <SelectItem v-for="c in textColumnsOfSelectedTable" :key="c.name" :value="c.name">{{ c.name }} ({{ c.data_type }})</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>
                <p class="text-xs text-muted-foreground">{{ $t('knowledgeBase.useExistingTableHint', 'The chatbot will search this table directly. Make sure its vectors were created with an OpenAI embedding model — a different model\'s vectors are not comparable to the ones this app generates for each customer message.') }}</p>
              </div>
            </div>

            <p v-if="isNew && !useExistingTable" class="text-xs text-muted-foreground -mt-2">{{ $t('knowledgeBase.collectionNameHint') }}</p>
            <p v-else-if="!isNew" class="text-xs text-muted-foreground -mt-2">{{ $t('knowledgeBase.readOnlyFieldsHint') }}</p>

            <div v-if="!useExistingTable" class="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <div class="space-y-1.5">
                <Label class="text-xs">{{ $t('knowledgeBase.embeddingDims') }}</Label>
                <Input v-if="isNew" v-model.number="form.embedding_dims" type="number" />
                <Input v-else :model-value="kb?.embedding_dims" readonly disabled class="text-muted-foreground" />
              </div>
              <div class="space-y-1.5">
                <Label class="text-xs">{{ $t('knowledgeBase.chunkSize') }}</Label>
                <Input v-model.number="form.chunk_size" type="number" />
              </div>
              <div class="space-y-1.5">
                <Label class="text-xs">{{ $t('knowledgeBase.chunkOverlap') }}</Label>
                <Input v-model.number="form.chunk_overlap" type="number" />
              </div>
            </div>
            <p v-if="!useExistingTable" class="text-xs text-muted-foreground -mt-2">{{ $t('knowledgeBase.chunkingHint') }}</p>
          </CardContent>
        </Card>

        <!-- Documents (edit mode only) -->
        <Card v-if="!isNew">
          <CardHeader class="pb-3">
            <CardTitle class="text-sm font-medium">{{ $t('knowledgeBase.documents') }} ({{ documents.length }})</CardTitle>
          </CardHeader>
          <CardContent class="space-y-4">
            <div v-if="isExternalTable" class="rounded-lg border border-border/60 p-3 text-sm text-muted-foreground">
              {{ $t('knowledgeBase.externalTableHint', "This knowledge base points at your own existing table — it's managed outside Whatomate, so documents can't be uploaded here. The chatbot searches it directly.") }}
            </div>
            <div v-else-if="!hasEmbeddingsKey" class="rounded-lg border border-amber-800 bg-amber-950/30 light:border-amber-200 light:bg-amber-50 p-3 flex items-start gap-2">
              <AlertTriangle class="h-4 w-4 text-amber-500 mt-0.5 shrink-0" />
              <div class="text-sm">
                <p class="text-amber-500">{{ $t('knowledgeBase.noEmbeddingsKey') }}</p>
                <RouterLink to="/settings" class="text-amber-400 underline hover:opacity-80">{{ $t('knowledgeBase.goToSettings') }}</RouterLink>
              </div>
            </div>

            <Tabs v-else v-model="addMode">
              <TabsList>
                <TabsTrigger value="text"><FileText class="h-3.5 w-3.5 mr-1.5" />{{ $t('knowledgeBase.pasteText') }}</TabsTrigger>
                <TabsTrigger value="upload"><Upload class="h-3.5 w-3.5 mr-1.5" />{{ $t('knowledgeBase.uploadFile') }}</TabsTrigger>
              </TabsList>
              <TabsContent value="text" class="space-y-2 pt-2">
                <Input v-model="pasteForm.name" :placeholder="$t('knowledgeBase.documentNamePlaceholder')" />
                <Textarea v-model="pasteForm.text" :rows="4" :placeholder="$t('knowledgeBase.pasteTextPlaceholder')" />
                <div class="flex justify-end">
                  <Button size="sm" :disabled="isAddingDocument || !pasteForm.name.trim() || !pasteForm.text.trim()" @click="addTextDocument">
                    <Loader2 v-if="isAddingDocument" class="h-4 w-4 mr-1 animate-spin" />
                    {{ $t('knowledgeBase.addDocument') }}
                  </Button>
                </div>
              </TabsContent>
              <TabsContent value="upload" class="space-y-2 pt-2">
                <Input v-model="uploadForm.name" :placeholder="$t('knowledgeBase.documentNameOptionalPlaceholder')" />
                <input ref="fileInput" type="file" accept=".txt,.md" class="text-sm text-muted-foreground file:mr-3 file:rounded-md file:border file:border-input file:bg-background file:px-3 file:py-1.5 file:text-sm" @change="handleFileChange" />
                <p class="text-xs text-muted-foreground">{{ $t('knowledgeBase.fileTypeHint') }}</p>
                <div class="flex justify-end">
                  <Button size="sm" :disabled="isAddingDocument || !uploadForm.file" @click="addUploadDocument">
                    <Loader2 v-if="isAddingDocument" class="h-4 w-4 mr-1 animate-spin" />
                    {{ $t('knowledgeBase.addDocument') }}
                  </Button>
                </div>
              </TabsContent>
            </Tabs>

            <DataTable
              :items="documents"
              :columns="documentColumns"
              :is-loading="isDocumentsLoading"
              :empty-title="$t('knowledgeBase.noDocumentsYet')"
            >
              <template #cell-name="{ item }">
                <div>
                  <span class="font-medium">{{ item.name }}</span>
                  <p v-if="item.status === 'failed' && item.error_message" class="text-xs text-destructive mt-0.5 max-w-md">{{ item.error_message }}</p>
                </div>
              </template>
              <template #cell-source="{ item }">
                <Badge variant="outline" class="text-xs">{{ item.source_type === 'upload' ? $t('knowledgeBase.sourceUpload') : $t('knowledgeBase.sourceText') }}</Badge>
              </template>
              <template #cell-status="{ item }">
                <div class="flex items-center gap-1.5">
                  <Loader2 v-if="item.status === 'processing'" class="h-3.5 w-3.5 animate-spin text-muted-foreground" />
                  <Badge :variant="statusVariant(item.status)">{{ statusLabel(item.status) }}</Badge>
                </div>
              </template>
              <template #cell-chunks="{ item }"><span class="text-muted-foreground">{{ item.status === 'completed' ? item.chunk_count : '—' }}</span></template>
              <template #cell-created="{ item }"><span class="text-muted-foreground">{{ formatDate(item.created_at) }}</span></template>
              <template #cell-actions="{ item }">
                <div class="flex items-center justify-end gap-1">
                  <IconButton v-if="item.status === 'failed'" :icon="RotateCw" :label="$t('knowledgeBase.retry')" class="h-8 w-8" :loading="retryingId === item.id" @click="retryDocument(item)" />
                  <IconButton :icon="Trash2" :label="$t('common.delete')" class="h-8 w-8 text-destructive" @click="openDeleteDocumentDialog(item)" />
                </div>
              </template>
            </DataTable>
          </CardContent>
        </Card>

        <!-- Chatbot Usage (edit mode only) — wires this KB into chatbot AI
             responses without needing to separately visit AI Contexts. -->
        <Card v-if="!isNew">
          <CardHeader class="pb-3">
            <CardTitle class="text-sm font-medium flex items-center gap-2">
              <MessageSquare class="h-4 w-4" />{{ $t('knowledgeBase.chatbotUsage', 'Use in Chatbot') }}
            </CardTitle>
          </CardHeader>
          <CardContent class="space-y-4">
            <p class="text-xs text-muted-foreground">{{ $t('knowledgeBase.chatbotUsageHint', "When enabled, the customer's message is matched against this knowledge base and the best-matching content is given to the AI as context.") }}</p>

            <div class="flex items-center gap-2">
              <Switch v-model:checked="chatbotEnabled" />
              <Label class="text-xs">{{ $t('knowledgeBase.enableInChatbot', 'Enabled') }}</Label>
            </div>

            <div class="grid grid-cols-2 gap-4">
              <div class="space-y-1.5">
                <Label class="text-xs">{{ $t('aiContexts.topK', 'Chunks to retrieve') }}</Label>
                <Input v-model.number="chatbotTopK" type="number" min="1" max="20" />
              </div>
              <div class="space-y-1.5">
                <Label class="text-xs">{{ $t('aiContexts.similarityThreshold', 'Similarity threshold') }}</Label>
                <Input v-model.number="chatbotThreshold" type="number" min="0" max="1" step="0.05" />
              </div>
            </div>

            <div class="flex justify-end">
              <Button size="sm" :disabled="isSavingChatbotUsage" @click="saveChatbotUsage">
                <Loader2 v-if="isSavingChatbotUsage" class="h-4 w-4 mr-1 animate-spin" />
                <Save v-else class="h-4 w-4 mr-1" />
                {{ $t('common.save') }}
              </Button>
            </div>
          </CardContent>
        </Card>
      </template>

      <template v-if="!isNew" #sidebar>
        <MetadataPanel :created-at="kb?.created_at" :updated-at="kb?.updated_at" />
      </template>
    </DetailPageLayout>

    <DeleteConfirmDialog
      v-model:open="deleteDialogOpen"
      :title="$t('knowledgeBase.deleteKnowledgeBase')"
      :description="$t('knowledgeBase.deleteKnowledgeBaseWarning')"
      :item-name="kb?.name"
      :is-submitting="isDeleting"
      @confirm="deleteKB"
    />

    <DeleteConfirmDialog
      v-model:open="deleteDocumentDialogOpen"
      :title="$t('knowledgeBase.deleteDocument')"
      :item-name="documentToDelete?.name"
      :is-submitting="isDeletingDocument"
      @confirm="confirmDeleteDocument"
    />

    <UnsavedChangesDialog :open="showLeaveDialog" @stay="cancelLeave" @leave="confirmLeave" />

    <!-- Inline Add Data Connection dialog -->
    <Dialog v-model:open="isAddConnectionDialogOpen">
      <DialogContent class="max-w-md">
        <DialogHeader>
          <DialogTitle>{{ $t('dataConnections.newConnection') }}</DialogTitle>
          <DialogDescription>{{ $t('knowledgeBase.addConnectionDialogHint', 'Add your database credentials once — reuse them across any knowledge base.') }}</DialogDescription>
        </DialogHeader>
        <div class="space-y-4 max-h-[60vh] overflow-y-auto pr-1">
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('dataConnections.name') }} <span class="text-destructive">*</span></Label>
            <Input v-model="connForm.name" :placeholder="$t('dataConnections.namePlaceholder')" />
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('dataConnections.connectionType') }}</Label>
            <RadioGroup v-model="connForm.type" class="flex flex-col gap-2">
              <div class="flex items-center space-x-2">
                <RadioGroupItem value="postgres" id="conn-type-postgres" />
                <Label for="conn-type-postgres" class="cursor-pointer font-normal">{{ $t('dataConnections.typePostgres') }}</Label>
              </div>
              <div class="flex items-center space-x-2">
                <RadioGroupItem value="qdrant" id="conn-type-qdrant" />
                <Label for="conn-type-qdrant" class="cursor-pointer font-normal">{{ $t('dataConnections.typeQdrant') }}</Label>
              </div>
            </RadioGroup>
          </div>

          <template v-if="connForm.type === 'postgres'">
            <div class="grid grid-cols-3 gap-3">
              <div class="col-span-2 space-y-1.5">
                <Label class="text-xs">{{ $t('dataConnections.host') }} <span class="text-destructive">*</span></Label>
                <Input v-model="connForm.host" :placeholder="$t('dataConnections.hostPlaceholder')" />
              </div>
              <div class="space-y-1.5">
                <Label class="text-xs">{{ $t('dataConnections.port') }}</Label>
                <Input v-model.number="connForm.port" type="number" />
              </div>
            </div>
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('dataConnections.database') }} <span class="text-destructive">*</span></Label>
              <Input v-model="connForm.database" :placeholder="$t('dataConnections.databasePlaceholder')" />
            </div>
            <div class="grid grid-cols-2 gap-3">
              <div class="space-y-1.5">
                <Label class="text-xs">{{ $t('dataConnections.username') }} <span class="text-destructive">*</span></Label>
                <Input v-model="connForm.username" />
              </div>
              <div class="space-y-1.5">
                <Label class="text-xs">{{ $t('dataConnections.password') }} <span class="text-destructive">*</span></Label>
                <Input v-model="connForm.password" type="password" />
              </div>
            </div>
          </template>
          <template v-else>
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('dataConnections.qdrantUrl') }} <span class="text-destructive">*</span></Label>
              <Input v-model="connForm.qdrant_url" :placeholder="$t('dataConnections.qdrantUrlPlaceholder')" />
            </div>
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('dataConnections.qdrantApiKey') }}</Label>
              <Input v-model="connForm.qdrant_api_key" type="password" />
            </div>
          </template>
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" @click="isAddConnectionDialogOpen = false">{{ $t('common.cancel') }}</Button>
          <Button size="sm" :disabled="isCreatingConnection" @click="createConnectionInline">
            <Loader2 v-if="isCreatingConnection" class="h-4 w-4 mr-1 animate-spin" />
            {{ $t('common.create') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
