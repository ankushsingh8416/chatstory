<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ordersService, type Order, type OrderStatus } from '@/services/api'
import { toast } from 'vue-sonner'
import { PageHeader, DataTable, type Column } from '@/components/shared'
import { getErrorMessage } from '@/lib/api-utils'
import { formatDate, formatCurrency } from '@/lib/utils'
import { ShoppingCart, Clock, CreditCard, CheckCircle, Truck, PackageCheck, XCircle } from 'lucide-vue-next'

const { t } = useI18n()

const orders = ref<Order[]>([])
const isLoading = ref(true)
const filterStatus = ref<string>('all')

const columns = computed<Column<Order>[]>(() => [
  { key: 'contact', label: t('orders.customer', 'Customer') },
  { key: 'items', label: t('orders.items', 'Items') },
  { key: 'subtotal', label: t('orders.total', 'Total') },
  { key: 'status', label: t('common.status') },
  { key: 'payment', label: t('orders.payment', 'Payment') },
  { key: 'created_at', label: t('common.created', 'Created') },
])

const statusOptions = computed(() => [
  { value: 'all', label: t('orders.allStatuses', 'All statuses') },
  { value: 'new', label: t('orders.statusNew', 'New') },
  { value: 'payment_pending', label: t('orders.statusPaymentPending', 'Payment Pending') },
  { value: 'confirmed', label: t('orders.statusConfirmed', 'Confirmed') },
  { value: 'shipped', label: t('orders.statusShipped', 'Shipped') },
  { value: 'delivered', label: t('orders.statusDelivered', 'Delivered') },
  { value: 'cancelled', label: t('orders.statusCancelled', 'Cancelled') },
])

async function fetchOrders() {
  isLoading.value = true
  try {
    const params = filterStatus.value !== 'all' ? { status: filterStatus.value } : undefined
    const response = await ordersService.list(params)
    const data = (response.data as any).data || response.data
    orders.value = data.orders || []
  } catch (e) {
    toast.error(getErrorMessage(e, t('orders.fetchFailed', 'Failed to load orders')))
  } finally {
    isLoading.value = false
  }
}

onMounted(fetchOrders)
watch(filterStatus, fetchOrders)

function statusIcon(status: OrderStatus) {
  switch (status) {
    case 'payment_pending': return CreditCard
    case 'confirmed': return CheckCircle
    case 'shipped': return Truck
    case 'delivered': return PackageCheck
    case 'cancelled': return XCircle
    default: return Clock
  }
}

function statusClass(status: OrderStatus): string {
  switch (status) {
    case 'confirmed': return 'border-blue-600 text-blue-600'
    case 'shipped': return 'border-amber-600 text-amber-600'
    case 'delivered': return 'border-green-600 text-green-600'
    case 'cancelled': return 'border-destructive text-destructive'
    default: return ''
  }
}
</script>

<template>
  <div class="flex flex-col h-full bg-[#0a0a0b] light:bg-gray-50">
    <PageHeader
      :title="$t('orders.title', 'Orders')"
      :subtitle="$t('orders.subtitle', 'Orders placed by customers via WhatsApp catalog')"
      :icon="ShoppingCart"
      icon-gradient="bg-gradient-to-br from-emerald-500 to-teal-600 shadow-emerald-500/20"
    />

    <ScrollArea class="flex-1">
      <div class="p-6">
        <Card>
          <CardHeader>
            <div class="flex items-center justify-between flex-wrap gap-4">
              <div>
                <CardTitle>{{ $t('orders.yourOrders', 'Your Orders') }}</CardTitle>
                <CardDescription>{{ $t('orders.yourOrdersDesc', 'Orders captured automatically when a customer checks out via WhatsApp.') }}</CardDescription>
              </div>
              <Select v-model="filterStatus">
                <SelectTrigger class="w-[160px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="opt in statusOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </CardHeader>
          <CardContent>
            <DataTable
              :items="orders"
              :columns="columns"
              :is-loading="isLoading"
              :empty-icon="ShoppingCart"
              :empty-title="$t('orders.noOrdersYet', 'No orders yet')"
              :empty-description="$t('orders.noOrdersYetDesc', 'Orders will appear here once customers start checking out via your WhatsApp catalog.')"
              item-name="orders"
            >
              <template #cell-contact="{ item }">
                <RouterLink :to="`/orders/${item.id}`" class="font-medium text-inherit no-underline hover:opacity-80">
                  {{ item.contact_name || item.contact_phone || item.contact_id }}
                </RouterLink>
              </template>
              <template #cell-items="{ item }"><span class="text-sm text-muted-foreground">{{ item.items?.length || 0 }}</span></template>
              <template #cell-subtotal="{ item }"><span>{{ formatCurrency(item.subtotal, item.currency) }}</span></template>
              <template #cell-status="{ item }">
                <Badge variant="outline" :class="[statusClass(item.status), 'text-xs']">
                  <component :is="statusIcon(item.status)" class="h-3 w-3 mr-1" />
                  {{ item.status.replace('_', ' ') }}
                </Badge>
              </template>
              <template #cell-payment="{ item }">
                <span class="text-xs text-muted-foreground">{{ item.payment_method || '—' }} / {{ item.payment_status }}</span>
              </template>
              <template #cell-created_at="{ item }"><span class="text-muted-foreground text-sm">{{ formatDate(item.created_at) }}</span></template>
            </DataTable>
          </CardContent>
        </Card>
      </div>
    </ScrollArea>
  </div>
</template>
