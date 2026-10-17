<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { catalogsService, type Catalog, type CatalogProduct } from '@/services/api'
import { toast } from 'vue-sonner'
import { getErrorMessage } from '@/lib/api-utils'
import { formatCurrency } from '@/lib/utils'
import { DetailPageLayout, MetadataPanel, DeleteConfirmDialog, DataTable, IconButton, type Column } from '@/components/shared'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ShoppingBag, Plus, Pencil, Trash2, Loader2 } from 'lucide-vue-next'

const route = useRoute()
const { t } = useI18n()

const catalogId = computed(() => route.params.id as string)
const catalog = ref<Catalog | null>(null)
const products = ref<CatalogProduct[]>([])
const isLoading = ref(true)
const isNotFound = ref(false)

const breadcrumbs = computed(() => [
  { label: t('catalogs.title', 'Catalogs'), href: '/catalogs' },
  { label: catalog.value?.name || '' },
])

const productColumns = computed<Column<CatalogProduct>[]>(() => [
  { key: 'name', label: t('common.name') },
  { key: 'price', label: t('catalogs.price', 'Price') },
  { key: 'retailer_id', label: t('catalogs.sku', 'SKU') },
  { key: 'status', label: t('common.status') },
  { key: 'actions', label: t('common.actions'), align: 'right' },
])

async function load() {
  isLoading.value = true
  isNotFound.value = false
  try {
    const response = await catalogsService.get(catalogId.value)
    const data = (response.data as any).data || response.data
    catalog.value = data
    await loadProducts()
  } catch {
    isNotFound.value = true
  } finally {
    isLoading.value = false
  }
}

async function loadProducts() {
  try {
    const response = await catalogsService.listProducts(catalogId.value)
    const data = (response.data as any).data || response.data
    products.value = data.products || []
  } catch (e) {
    toast.error(getErrorMessage(e, t('catalogs.productsFetchFailed', 'Failed to load products')))
  }
}

onMounted(load)

// --- Create/edit product ---
const productDialogOpen = ref(false)
const isSavingProduct = ref(false)
const editingProduct = ref<CatalogProduct | null>(null)
const productForm = ref({ name: '', description: '', price: 0, currency: 'INR', url: '', image_url: '', retailer_id: '' })

function openCreateProduct() {
  editingProduct.value = null
  productForm.value = { name: '', description: '', price: 0, currency: 'INR', url: '', image_url: '', retailer_id: '' }
  productDialogOpen.value = true
}

function openEditProduct(product: CatalogProduct) {
  editingProduct.value = product
  productForm.value = {
    name: product.name,
    description: product.description,
    price: product.price / 100,
    currency: product.currency,
    url: product.url,
    image_url: product.image_url,
    retailer_id: product.retailer_id,
  }
  productDialogOpen.value = true
}

async function saveProduct() {
  if (!productForm.value.name.trim() || productForm.value.price <= 0) {
    toast.error(t('catalogs.productNamePriceRequired', 'Name and price are required'))
    return
  }
  isSavingProduct.value = true
  try {
    // price is entered as a decimal amount (e.g. 499.00) but stored in the
    // smallest currency unit (paise/cents) — matches how synced Meta prices are stored.
    const payload = { ...productForm.value, price: Math.round(productForm.value.price * 100) }
    if (editingProduct.value) {
      await catalogsService.updateProduct(editingProduct.value.id, payload)
      toast.success(t('common.updatedSuccess', { resource: t('resources.Product', 'Product') }))
    } else {
      await catalogsService.createProduct(catalogId.value, payload)
      toast.success(t('common.createdSuccess', { resource: t('resources.Product', 'Product') }))
    }
    productDialogOpen.value = false
    await loadProducts()
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedSave', { resource: t('resources.product', 'product') })))
  } finally {
    isSavingProduct.value = false
  }
}

// --- Delete product ---
const deleteDialogOpen = ref(false)
const productToDelete = ref<CatalogProduct | null>(null)
const isDeleting = ref(false)

function openDeleteDialog(product: CatalogProduct) {
  productToDelete.value = product
  deleteDialogOpen.value = true
}

async function confirmDelete() {
  if (!productToDelete.value) return
  isDeleting.value = true
  try {
    await catalogsService.deleteProduct(productToDelete.value.id)
    toast.success(t('common.deletedSuccess', { resource: t('resources.Product', 'Product') }))
    deleteDialogOpen.value = false
    productToDelete.value = null
    await loadProducts()
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedDelete', { resource: t('resources.product', 'product') })))
  } finally {
    isDeleting.value = false
  }
}
</script>

<template>
  <div class="h-full">
    <DetailPageLayout
      :title="catalog?.name || ''"
      :icon="ShoppingBag"
      icon-gradient="bg-gradient-to-br from-amber-500 to-orange-600 shadow-amber-500/20"
      back-link="/catalogs"
      :breadcrumbs="breadcrumbs"
      :is-loading="isLoading"
      :is-not-found="isNotFound"
      :not-found-title="$t('catalogs.notFound', 'Catalog not found')"
    >
      <Card>
        <CardHeader class="pb-3">
          <CardTitle class="text-sm font-medium">{{ $t('teams.details', 'Details') }}</CardTitle>
        </CardHeader>
        <CardContent class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('catalogs.account', 'WhatsApp Account') }}</Label>
            <Input :model-value="catalog?.whatsapp_account || ''" readonly disabled class="text-muted-foreground" />
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('catalogs.metaCatalogId', 'Meta Catalog ID') }}</Label>
            <Input :model-value="catalog?.meta_catalog_id || ''" readonly disabled class="text-muted-foreground font-mono text-xs" />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader class="pb-3">
          <div class="flex items-center justify-between">
            <CardTitle class="text-sm font-medium">{{ $t('catalogs.products', 'Products') }} ({{ products.length }})</CardTitle>
            <Button size="sm" @click="openCreateProduct">
              <Plus class="h-4 w-4 mr-1" />{{ $t('catalogs.addProduct', 'Add Product') }}
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <DataTable
            :items="products"
            :columns="productColumns"
            :empty-icon="ShoppingBag"
            :empty-title="$t('catalogs.noProductsYet', 'No products yet')"
          >
            <template #cell-name="{ item }">
              <div class="flex items-center gap-2">
                <img v-if="item.image_url" :src="item.image_url" class="h-8 w-8 rounded object-cover shrink-0" :alt="item.name" />
                <span class="font-medium">{{ item.name }}</span>
              </div>
            </template>
            <template #cell-price="{ item }"><span>{{ formatCurrency(item.price, item.currency) }}</span></template>
            <template #cell-retailer_id="{ item }"><span class="text-xs font-mono text-muted-foreground">{{ item.retailer_id || '—' }}</span></template>
            <template #cell-status="{ item }">
              <Badge :variant="item.is_active ? 'success' : 'secondary'">{{ item.is_active ? $t('common.active') : $t('common.inactive', 'Inactive') }}</Badge>
            </template>
            <template #cell-actions="{ item }">
              <div class="flex items-center justify-end gap-1">
                <IconButton :icon="Pencil" :label="$t('common.edit')" class="h-8 w-8" @click="openEditProduct(item)" />
                <IconButton :icon="Trash2" :label="$t('common.delete')" class="h-8 w-8 text-destructive" @click="openDeleteDialog(item)" />
              </div>
            </template>
          </DataTable>
        </CardContent>
      </Card>

      <template #sidebar>
        <MetadataPanel :created-at="catalog?.created_at" :updated-at="catalog?.updated_at" />
      </template>
    </DetailPageLayout>

    <DeleteConfirmDialog
      v-model:open="deleteDialogOpen"
      :title="$t('catalogs.deleteProduct', 'Delete Product')"
      :item-name="productToDelete?.name"
      :is-submitting="isDeleting"
      @confirm="confirmDelete"
    />

    <Dialog v-model:open="productDialogOpen">
      <DialogContent class="max-w-md">
        <DialogHeader>
          <DialogTitle>{{ editingProduct ? $t('catalogs.editProduct', 'Edit Product') : $t('catalogs.addProduct', 'Add Product') }}</DialogTitle>
          <DialogDescription>{{ $t('catalogs.productDialogDesc', 'Products are created and updated in Meta Commerce Manager too.') }}</DialogDescription>
        </DialogHeader>
        <div class="space-y-3 max-h-[60vh] overflow-y-auto pr-1">
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('common.name') }} <span class="text-destructive">*</span></Label>
            <Input v-model="productForm.name" />
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('common.description', 'Description') }}</Label>
            <Textarea v-model="productForm.description" :rows="2" />
          </div>
          <div class="grid grid-cols-2 gap-3">
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('catalogs.price', 'Price') }} <span class="text-destructive">*</span></Label>
              <Input v-model.number="productForm.price" type="number" min="0" step="0.01" />
            </div>
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('catalogs.currency', 'Currency') }}</Label>
              <Input v-model="productForm.currency" placeholder="INR" />
            </div>
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('catalogs.sku', 'SKU') }} / Retailer ID</Label>
            <Input v-model="productForm.retailer_id" />
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('catalogs.imageUrl', 'Image URL') }}</Label>
            <Input v-model="productForm.image_url" />
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('catalogs.productUrl', 'Product URL') }}</Label>
            <Input v-model="productForm.url" />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" @click="productDialogOpen = false">{{ $t('common.cancel') }}</Button>
          <Button size="sm" :disabled="isSavingProduct" @click="saveProduct">
            <Loader2 v-if="isSavingProduct" class="h-4 w-4 mr-1 animate-spin" />
            {{ $t('common.save') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
