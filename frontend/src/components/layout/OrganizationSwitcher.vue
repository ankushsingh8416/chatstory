<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useOrganizationsStore } from '@/stores/organizations'
import { useAuthStore } from '@/stores/auth'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Building2 } from 'lucide-vue-next'

const props = defineProps<{
  collapsed?: boolean
}>()

const organizationsStore = useOrganizationsStore()
const authStore = useAuthStore()

const isSuperAdmin = computed(() => authStore.user?.is_super_admin || false)

const shouldShowSwitcher = computed(() =>
  isSuperAdmin.value || organizationsStore.isMultiOrg
)

// Build the org list depending on user type. Logo thumbnails are only
// available for the super-admin path (organizationsStore.organizations
// carries the full Organization object) — the multi-org member path comes
// from a lighter /me/organizations response that doesn't include one.
const orgList = computed(() => {
  if (isSuperAdmin.value) {
    return organizationsStore.organizations.map(org => ({ id: org.id, name: org.name, logo: org.logo_data_url }))
  }
  return organizationsStore.myOrganizations.map(org => ({ id: org.organization_id, name: org.name, logo: undefined as string | undefined }))
})

const currentOrgId = computed(() => {
  if (isSuperAdmin.value) {
    return organizationsStore.selectedOrgId || ''
  }
  return authStore.user?.organization_id || ''
})

onMounted(async () => {
  // Fetch user's org memberships for all authenticated users
  await organizationsStore.fetchMyOrganizations()

  if (isSuperAdmin.value) {
    organizationsStore.init()
    await organizationsStore.fetchOrganizations()

    // If no org selected, default to user's own org
    if (!organizationsStore.selectedOrgId && authStore.user?.organization_id) {
      organizationsStore.selectOrganization(authStore.user.organization_id)
    }
  }
})

// Watch for auth changes
watch(() => authStore.user?.is_super_admin, async (superAdmin) => {
  if (superAdmin) {
    organizationsStore.init()
    await organizationsStore.fetchOrganizations()
  }
})

const handleOrgChange = async (value: string | number | bigint | Record<string, any> | null) => {
  if (!value || typeof value !== 'string') return

  if (isSuperAdmin.value) {
    // Super admins: set localStorage header and reload
    organizationsStore.selectOrganization(value)
    window.location.reload()
  } else {
    // Multi-org users: call switchOrg API for new JWT tokens, then reload
    try {
      await authStore.switchOrg(value)
      window.location.reload()
    } catch {
      // If switch fails, don't reload
    }
  }
}
</script>

<template>
  <div v-if="shouldShowSwitcher" class="px-2 py-2 border-b border-white/10">
    <div v-if="!collapsed" class="space-y-1">
      <span class="text-[11px] font-medium text-white/80 uppercase tracking-wide px-1 block">
        Organization
      </span>
      <Select
        v-if="orgList.length > 0"
        :model-value="currentOrgId"
        @update:model-value="handleOrgChange"
      >
        <SelectTrigger class="h-8 text-[13px] text-white data-[placeholder]:text-white/70 [&>svg]:text-white/70">
          <SelectValue placeholder="Select organization" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem
            v-for="org in orgList"
            :key="org.id"
            :value="org.id"
          >
            <div class="flex items-center gap-2">
              <span v-if="org.logo" class="h-3.5 w-3.5 rounded overflow-hidden shrink-0 flex items-center justify-center">
                <img :src="org.logo" :alt="org.name" class="h-full w-full object-contain" />
              </span>
              <Building2 v-else class="h-3.5 w-3.5 text-muted-foreground" />
              <span>{{ org.name }}</span>
            </div>
          </SelectItem>
        </SelectContent>
      </Select>
      <div v-else-if="organizationsStore.loading" class="text-[12px] text-white/80 px-1">
        Loading...
      </div>
      <div v-else-if="organizationsStore.error" class="text-[12px] text-red-300 px-1">
        {{ organizationsStore.error }}
      </div>
      <div v-else class="text-[12px] text-white/80 px-1">
        No organizations found
      </div>
    </div>

    <!-- Collapsed view - just show icon with selected org initial -->
    <div v-else class="flex justify-center">
      <Button
        variant="ghost"
        size="icon"
        class="h-8 w-8 text-white/90 hover:text-white hover:bg-white/[0.08]"
        :title="organizationsStore.selectedOrganization?.name || 'All Organizations'"
      >
        <Building2 class="h-4 w-4" />
      </Button>
    </div>
  </div>
</template>
