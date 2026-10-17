<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { catalogsService, accountsService, type Catalog } from '@/services/api'
import { toast } from 'vue-sonner'
import { PageHeader, DataTable, DeleteConfirmDialog, IconButton, type Column } from '@/components/shared'
import { getErrorMessage } from '@/lib/api-utils'
import { formatDate } from '@/lib/utils'
import { Plus, Trash2, ShoppingBag, RotateCw, Loader2 } from 'lucide-vue-next'

const { t } = useI18n()

const catalogs = ref<Catalog[]>([])
const accounts = ref<{ id: string; name: string }[]>([])
const isLoading = ref(true)
const isSyncing = ref(false)

const columns = computed<Column<Catalog>[]>(() => [
  { key: 'name', label: t('catalogs.name', 'Catalog') },
  { key: 'whatsapp_account', label: t('catalogs.account', 'WhatsApp Account') },
  { key: 'products', label: t('catalogs.products', 'Products') },
  { key: 'created_at', label: t('common.created', 'Created') },
  { key: 'actions', label: t('common.actions'), align: 'right' },
])

async function fetchCatalogs() {
  isLoading.value = true
  try {
    const response = await catalogsService.list()
    const data = (response.data as any).data || response.data
    catalogs.value = data.catalogs || []
  } catch (e) {
    toast.error(getErrorMessage(e, t('catalogs.fetchFailed', 'Failed to load catalogs')))
  } finally {
    isLoading.value = false
  }
}

async function fetchAccounts() {
  try {
    const response = await accountsService.list()
    const data = (response.data as any).data || response.data
    accounts.value = data.accounts || data || []
  } catch {
    // Non-fatal — sync/create dialogs just show an empty account picker.
  }
}

onMounted(async () => {
  await Promise.all([fetchCatalogs(), fetchAccounts()])
})

// --- Sync from Meta ---
const syncDialogOpen = ref(false)
const syncAccount = ref('')

function openSyncDialog() {
  syncAccount.value = accounts.value[0]?.name || ''
  syncDialogOpen.value = true
}

async function runSync() {
  if (!syncAccount.value) {
    toast.error(t('catalogs.selectAccountRequired', 'Select a WhatsApp account'))
    return
  }
  isSyncing.value = true
  try {
    const response = await catalogsService.sync(syncAccount.value)
    const data = (response.data as any).data || response.data
    toast.success(`${t('catalogs.syncedSuccess', 'Synced')} ${data.synced}/${data.total}`)
    syncDialogOpen.value = false
    await fetchCatalogs()
  } catch (e) {
    toast.error(getErrorMessage(e, t('catalogs.syncFailed', 'Failed to sync catalogs')))
  } finally {
    isSyncing.value = false
  }
}

// --- Delete ---
const deleteDialogOpen = ref(false)
const catalogToDelete = ref<Catalog | null>(null)
const isDeleting = ref(false)

function openDeleteDialog(catalog: Catalog) {
  catalogToDelete.value = catalog
  deleteDialogOpen.value = true
}

async function confirmDelete() {
  if (!catalogToDelete.value) return
  isDeleting.value = true
  try {
    await catalogsService.delete(catalogToDelete.value.id)
    toast.success(t('common.deletedSuccess', { resource: t('resources.Catalog', 'Catalog') }))
    deleteDialogOpen.value = false
    catalogToDelete.value = null
    await fetchCatalogs()
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedDelete', { resource: t('resources.catalog', 'catalog') })))
  } finally {
    isDeleting.value = false
  }
}
</script>

<template>
  <div class="flex flex-col h-full bg-[#0a0a0b] light:bg-gray-50">
    <PageHeader
      :title="$t('catalogs.title', 'Catalogs')"
      :subtitle="$t('catalogs.subtitle', 'Product catalogs synced from Meta Commerce Manager')"
      :icon="ShoppingBag"
      icon-gradient="bg-gradient-to-br from-amber-500 to-orange-600 shadow-amber-500/20"
    >
      <template #actions>
        <Button variant="outline" size="sm" @click="openSyncDialog">
          <RotateCw class="h-4 w-4 mr-2" />
          {{ $t('catalogs.syncFromMeta', 'Sync from Meta') }}
        </Button>
      </template>
    </PageHeader>

    <ScrollArea class="flex-1">
      <div class="p-6">
        <Card>
          <CardHeader>
            <CardTitle>{{ $t('catalogs.yourCatalogs', 'Your Catalogs') }}</CardTitle>
            <CardDescription>{{ $t('catalogs.yourCatalogsDesc', "Catalogs created in Meta Commerce Manager for your business's WhatsApp numbers.") }}</CardDescription>
          </CardHeader>
          <CardContent>
            <DataTable
              :items="catalogs"
              :columns="columns"
              :is-loading="isLoading"
              :empty-icon="ShoppingBag"
              :empty-title="$t('catalogs.noCatalogsYet', 'No catalogs yet')"
              :empty-description="$t('catalogs.noCatalogsYetDesc', 'Sync from Meta Commerce Manager to bring your catalogs in.')"
              item-name="catalogs"
            >
              <template #cell-name="{ item }">
                <RouterLink :to="`/catalogs/${item.id}`" class="font-medium text-inherit no-underline hover:opacity-80">{{ item.name }}</RouterLink>
              </template>
              <template #cell-whatsapp_account="{ item }">
                <span class="text-sm text-muted-foreground">{{ item.whatsapp_account || '—' }}</span>
              </template>
              <template #cell-products="{ item }">
                <Badge variant="outline" class="text-xs">{{ item.product_count }}</Badge>
              </template>
              <template #cell-created_at="{ item }">
                <span class="text-muted-foreground text-sm">{{ formatDate(item.created_at) }}</span>
              </template>
              <template #cell-actions="{ item }">
                <div class="flex items-center justify-end gap-1">
                  <IconButton :icon="Trash2" :label="$t('common.delete')" class="h-8 w-8 text-destructive" @click="openDeleteDialog(item)" />
                </div>
              </template>
              <template #empty-action>
                <Button variant="outline" size="sm" @click="openSyncDialog">
                  <Plus class="h-4 w-4 mr-2" />
                  {{ $t('catalogs.syncFromMeta', 'Sync from Meta') }}
                </Button>
              </template>
            </DataTable>
          </CardContent>
        </Card>
      </div>
    </ScrollArea>

    <DeleteConfirmDialog
      v-model:open="deleteDialogOpen"
      :title="$t('catalogs.deleteCatalog', 'Delete Catalog')"
      :item-name="catalogToDelete?.name"
      :is-submitting="isDeleting"
      @confirm="confirmDelete"
    />

    <Dialog v-model:open="syncDialogOpen">
      <DialogContent class="max-w-sm">
        <DialogHeader>
          <DialogTitle>{{ $t('catalogs.syncFromMeta', 'Sync from Meta') }}</DialogTitle>
          <DialogDescription>{{ $t('catalogs.syncDialogDesc', "Pulls catalogs linked to this WhatsApp number's Meta Business account.") }}</DialogDescription>
        </DialogHeader>
        <div class="space-y-1.5">
          <Label class="text-xs">{{ $t('catalogs.account', 'WhatsApp Account') }}</Label>
          <Select v-model="syncAccount">
            <SelectTrigger><SelectValue :placeholder="$t('catalogs.selectAccount', 'Select account')" /></SelectTrigger>
            <SelectContent>
              <SelectItem v-for="a in accounts" :key="a.id" :value="a.name">{{ a.name }}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" @click="syncDialogOpen = false">{{ $t('common.cancel') }}</Button>
          <Button size="sm" :disabled="isSyncing" @click="runSync">
            <Loader2 v-if="isSyncing" class="h-4 w-4 mr-1 animate-spin" />
            {{ $t('common.sync', 'Sync') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
