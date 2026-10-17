<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ordersService, type Order, type OrderStatus } from '@/services/api'
import { toast } from 'vue-sonner'
import { getErrorMessage } from '@/lib/api-utils'
import { formatDateTime, formatCurrency } from '@/lib/utils'
import { DetailPageLayout, MetadataPanel } from '@/components/shared'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import { ShoppingCart, Loader2, Truck, Ban } from 'lucide-vue-next'

const route = useRoute()
const { t } = useI18n()

const orderId = computed(() => route.params.id as string)
const order = ref<Order | null>(null)
const isLoading = ref(true)
const isNotFound = ref(false)
const isSaving = ref(false)

const breadcrumbs = computed(() => [
  { label: t('orders.title', 'Orders'), href: '/orders' },
  { label: order.value ? `#${order.value.id.slice(0, 8)}` : '' },
])

const trackingForm = ref({ tracking_carrier: '', tracking_number: '', tracking_url: '' })

async function load() {
  isLoading.value = true
  isNotFound.value = false
  try {
    const response = await ordersService.get(orderId.value)
    const data = (response.data as any).data || response.data
    order.value = data
    trackingForm.value = {
      tracking_carrier: data.tracking_carrier || '',
      tracking_number: data.tracking_number || '',
      tracking_url: data.tracking_url || '',
    }
  } catch {
    isNotFound.value = true
  } finally {
    isLoading.value = false
  }
}

onMounted(load)

const canConfirm = computed(() => order.value?.status === 'new' || order.value?.status === 'payment_pending')
const canShip = computed(() => order.value?.status === 'confirmed')
const canDeliver = computed(() => order.value?.status === 'shipped')
const canCancel = computed(() => order.value && !['delivered', 'cancelled'].includes(order.value.status))

async function setStatus(status: OrderStatus, extra?: Record<string, string>) {
  if (!order.value) return
  isSaving.value = true
  try {
    const response = await ordersService.updateStatus(order.value.id, { status, ...extra })
    const data = (response.data as any).data || response.data
    order.value = data
    toast.success(t('orders.statusUpdated', 'Order status updated'))
  } catch (e) {
    toast.error(getErrorMessage(e, t('orders.updateFailed', 'Failed to update order')))
  } finally {
    isSaving.value = false
  }
}

async function saveTracking() {
  await setStatus('shipped', trackingForm.value)
}

const statusLabel = computed(() => order.value?.status.replace('_', ' ') || '')
const statusBadgeClass = computed(() => {
  switch (order.value?.status) {
    case 'confirmed': return 'border-blue-600 text-blue-600'
    case 'shipped': return 'border-amber-600 text-amber-600'
    case 'delivered': return 'border-green-600 text-green-600'
    case 'cancelled': return 'border-destructive text-destructive'
    default: return ''
  }
})
</script>

<template>
  <div class="h-full">
    <DetailPageLayout
      :title="order ? `Order #${order.id.slice(0, 8)}` : ''"
      :icon="ShoppingCart"
      icon-gradient="bg-gradient-to-br from-emerald-500 to-teal-600 shadow-emerald-500/20"
      back-link="/orders"
      :breadcrumbs="breadcrumbs"
      :is-loading="isLoading"
      :is-not-found="isNotFound"
      :not-found-title="$t('orders.notFound', 'Order not found')"
    >
      <template #actions>
        <div v-if="order" class="flex items-center gap-2">
          <Badge variant="outline" :class="[statusBadgeClass]">{{ statusLabel }}</Badge>
          <Button v-if="canConfirm" size="sm" :disabled="isSaving" @click="setStatus('confirmed')">{{ $t('orders.markConfirmed', 'Mark Confirmed') }}</Button>
          <Button v-if="canDeliver" size="sm" :disabled="isSaving" @click="setStatus('delivered')">{{ $t('orders.markDelivered', 'Mark Delivered') }}</Button>
          <Button v-if="canCancel" variant="outline" size="sm" :disabled="isSaving" @click="setStatus('cancelled')">
            <Ban class="h-4 w-4 mr-1" />{{ $t('orders.cancel', 'Cancel') }}
          </Button>
        </div>
      </template>

      <Card>
        <CardHeader class="pb-3">
          <CardTitle class="text-sm font-medium">{{ $t('orders.customer', 'Customer') }}</CardTitle>
        </CardHeader>
        <CardContent class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('common.name') }}</Label>
            <Input :model-value="order?.contact_name || '—'" readonly disabled class="text-muted-foreground" />
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('common.phone', 'Phone') }}</Label>
            <Input :model-value="order?.contact_phone || '—'" readonly disabled class="text-muted-foreground" />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader class="pb-3">
          <CardTitle class="text-sm font-medium">{{ $t('orders.items', 'Items') }}</CardTitle>
        </CardHeader>
        <CardContent class="space-y-2">
          <div v-for="item in order?.items" :key="item.id" class="flex items-center justify-between py-1.5 text-sm">
            <div>
              <span class="font-medium">{{ item.product_name }}</span>
              <span class="text-muted-foreground ml-2">x{{ item.quantity }}</span>
            </div>
            <span>{{ formatCurrency(item.unit_price * item.quantity, item.currency) }}</span>
          </div>
          <Separator class="my-2" />
          <div class="flex items-center justify-between font-medium">
            <span>{{ $t('orders.total', 'Total') }}</span>
            <span>{{ order ? formatCurrency(order.subtotal, order.currency) : '' }}</span>
          </div>
          <p v-if="order?.customer_note" class="text-xs text-muted-foreground pt-2">{{ $t('orders.note', 'Note') }}: {{ order.customer_note }}</p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader class="pb-3">
          <CardTitle class="text-sm font-medium">{{ $t('orders.paymentInfo', 'Payment') }}</CardTitle>
        </CardHeader>
        <CardContent class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('orders.method', 'Method') }}</Label>
            <Input :model-value="order?.payment_method || '—'" readonly disabled class="text-muted-foreground" />
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('orders.paymentStatus', 'Status') }}</Label>
            <Input :model-value="order?.payment_status || '—'" readonly disabled class="text-muted-foreground" />
          </div>
          <div v-if="order?.paid_at" class="space-y-1.5 sm:col-span-2">
            <Label class="text-xs">{{ $t('orders.paidAt', 'Paid at') }}</Label>
            <Input :model-value="formatDateTime(order.paid_at)" readonly disabled class="text-muted-foreground" />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader class="pb-3">
          <CardTitle class="text-sm font-medium flex items-center gap-2"><Truck class="h-4 w-4" />{{ $t('orders.tracking', 'Shipment Tracking') }}</CardTitle>
        </CardHeader>
        <CardContent class="space-y-3">
          <p class="text-xs text-muted-foreground">{{ $t('orders.trackingHint', 'Entered manually — saving here also marks the order Shipped and notifies the customer on WhatsApp.') }}</p>
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('orders.carrier', 'Carrier') }}</Label>
              <Input v-model="trackingForm.tracking_carrier" placeholder="e.g. Delhivery" />
            </div>
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('orders.trackingNumber', 'Tracking Number') }}</Label>
              <Input v-model="trackingForm.tracking_number" />
            </div>
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('orders.trackingUrl', 'Tracking URL') }}</Label>
              <Input v-model="trackingForm.tracking_url" />
            </div>
          </div>
          <div class="flex justify-end">
            <Button size="sm" :disabled="isSaving || !canShip" @click="saveTracking">
              <Loader2 v-if="isSaving" class="h-4 w-4 mr-1 animate-spin" />
              {{ $t('orders.saveAndShip', 'Save & Mark Shipped') }}
            </Button>
          </div>
        </CardContent>
      </Card>

      <template #sidebar>
        <MetadataPanel :created-at="order?.created_at" :updated-at="order?.updated_at" />
      </template>
    </DetailPageLayout>
  </div>
</template>
