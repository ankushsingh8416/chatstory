<script setup lang="ts">
import { ref, computed, onMounted, nextTick, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  organizationsService,
  organizationService,
  usersService,
  type Organization,
  type OrganizationMember,
} from '@/services/api'
import { toast } from 'vue-sonner'
import { getErrorMessage } from '@/lib/api-utils'
import { formatDate } from '@/lib/utils'
import { useUnsavedChangesGuard } from '@/composables/useUnsavedChangesGuard'
import { useOrganizationsStore } from '@/stores/organizations'
import { DetailPageLayout, MetadataPanel, ConfirmDialog, DeleteConfirmDialog, UnsavedChangesDialog, DataTable, IconButton, type Column } from '@/components/shared'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  Building2,
  Save,
  Trash2,
  Ban,
  CheckCircle,
  KeyRound,
  UserPlus,
  UserMinus,
  Copy,
  AlertTriangle,
  Upload,
  Loader2,
  Shield,
} from 'lucide-vue-next'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const organizationsStore = useOrganizationsStore()

const orgId = computed(() => route.params.id as string)
const isNew = computed(() => orgId.value === 'new')

const org = ref<Organization | null>(null)
const members = ref<OrganizationMember[]>([])
const isLoading = ref(true)
const isNotFound = ref(false)
const isSaving = ref(false)
const isMembersLoading = ref(false)
const hasChanges = ref(false)
const deleteDialogOpen = ref(false)
const isDeleting = ref(false)
const suspendDialogOpen = ref(false)
const isSuspending = ref(false)
const isUploadingLogo = ref(false)
const isRemovingLogo = ref(false)
const logoInput = ref<HTMLInputElement | null>(null)

const { showLeaveDialog, confirmLeave, cancelLeave } = useUnsavedChangesGuard(hasChanges)

// --- Create mode form ---
const createForm = ref({
  org_name: '',
  admin_email: '',
  admin_full_name: '',
  passwordMode: 'auto' as 'auto' | 'manual',
  admin_password: '',
})

watch(createForm, () => { if (isNew.value) hasChanges.value = true }, { deep: true })

// --- Edit mode form ---
const nameForm = ref({ name: '' })

watch(nameForm, () => { if (!isNew.value && org.value) hasChanges.value = true }, { deep: true })

const breadcrumbs = computed(() => [
  { label: t('nav.organizations'), href: '/admin/organizations' },
  { label: isNew.value ? t('admin.organizations.newOrganization') : (org.value?.name || '') },
])

async function loadOrg() {
  isLoading.value = true
  isNotFound.value = false
  try {
    const response = await organizationsService.list()
    const data = (response.data as any).data || response.data
    const orgs: Organization[] = data.organizations || []
    const found = orgs.find(o => o.id === orgId.value)
    if (!found) {
      isNotFound.value = true
      return
    }
    org.value = found
    nameForm.value = { name: found.name }
    nextTick(() => { hasChanges.value = false })
    await loadMembers()
  } catch {
    isNotFound.value = true
  } finally {
    isLoading.value = false
  }
}

async function loadMembers() {
  if (!org.value) return
  isMembersLoading.value = true
  try {
    const response = await organizationsService.listMembers(org.value.id)
    const data = (response.data as any).data || response.data
    members.value = data.members || []
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedLoad', { resource: t('resources.members') })))
  } finally {
    isMembersLoading.value = false
  }
}

async function saveName() {
  if (!org.value) return
  if (!nameForm.value.name.trim()) {
    toast.error(t('admin.organizations.orgNameRequired'))
    return
  }
  isSaving.value = true
  try {
    await organizationsService.update(org.value.id, { name: nameForm.value.name.trim() })
    toast.success(t('common.updatedSuccess', { resource: t('resources.Organization') }))
    hasChanges.value = false
    await loadOrg()
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedSave', { resource: t('resources.organization') })))
  } finally {
    isSaving.value = false
  }
}

// Logo upload targets org.value.id explicitly (via the X-Organization-ID
// override baked into organizationService.uploadLogo/deleteLogo) so this
// works for any org the super admin is viewing, without switching into it.
async function uploadOrgLogo(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input?.files?.[0]
  if (!file || !org.value) return

  if (file.size > 150 * 1024) {
    toast.error(t('admin.organizations.logoTooLarge'))
    input.value = ''
    return
  }

  isUploadingLogo.value = true
  try {
    const response = await organizationService.uploadLogo(file, org.value.id)
    const data = (response.data as any).data || response.data
    org.value = { ...org.value, logo_data_url: data.logo_data_url }
    toast.success(t('admin.organizations.logoUploaded'))
  } catch (e) {
    toast.error(getErrorMessage(e, t('admin.organizations.logoUploadFailed')))
  } finally {
    isUploadingLogo.value = false
    input.value = ''
  }
}

async function removeOrgLogo() {
  if (!org.value) return
  isRemovingLogo.value = true
  try {
    await organizationService.deleteLogo(org.value.id)
    org.value = { ...org.value, logo_data_url: undefined }
    toast.success(t('admin.organizations.logoRemoved'))
  } catch (e) {
    toast.error(getErrorMessage(e, t('admin.organizations.logoRemoveFailed')))
  } finally {
    isRemovingLogo.value = false
  }
}

// Switches the super admin's active org context (same mechanism as
// OrganizationSwitcher's dropdown) then hard-navigates into that org's
// Roles & Permissions page — reusing the existing per-org Roles feature
// rather than building a second permissions editor inside the admin panel.
function manageRolesAndPermissions() {
  if (!org.value) return
  organizationsStore.selectOrganization(org.value.id)
  window.location.href = '/settings/roles'
}

// --- Password reveal dialog (shared between create-with-admin and member reset-password) ---
const passwordDialogOpen = ref(false)
const passwordDialogEmail = ref('')
const passwordDialogPassword = ref('')
let passwordDialogOnClose: (() => void) | null = null

function showPasswordDialog(email: string, password: string, onClose?: () => void) {
  passwordDialogEmail.value = email
  passwordDialogPassword.value = password
  passwordDialogOnClose = onClose || null
  passwordDialogOpen.value = true
}

function closePasswordDialog() {
  passwordDialogOpen.value = false
  const cb = passwordDialogOnClose
  passwordDialogOnClose = null
  if (cb) cb()
}

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text)
  toast.success(t('common.copiedToClipboard'))
}

async function createOrg() {
  if (!createForm.value.org_name.trim()) {
    toast.error(t('admin.organizations.orgNameRequired'))
    return
  }
  if (!createForm.value.admin_email.trim()) {
    toast.error(t('admin.organizations.adminEmailRequired'))
    return
  }
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(createForm.value.admin_email.trim())) {
    toast.error(t('validation.email'))
    return
  }
  if (!createForm.value.admin_full_name.trim()) {
    toast.error(t('admin.organizations.adminFullNameRequired'))
    return
  }
  if (createForm.value.passwordMode === 'manual' && createForm.value.admin_password.trim().length < 8) {
    toast.error(t('auth.passwordTooShort'))
    return
  }

  isSaving.value = true
  try {
    const payload: { org_name: string; admin_email: string; admin_full_name: string; admin_password?: string } = {
      org_name: createForm.value.org_name.trim(),
      admin_email: createForm.value.admin_email.trim(),
      admin_full_name: createForm.value.admin_full_name.trim(),
    }
    if (createForm.value.passwordMode === 'manual') {
      payload.admin_password = createForm.value.admin_password
    }
    const response = await organizationsService.createWithAdmin(payload)
    const created = (response.data as any).data || response.data
    hasChanges.value = false
    toast.success(t('common.createdSuccess', { resource: t('resources.Organization') }))

    if (created.generated_password) {
      showPasswordDialog(created.admin_email, created.generated_password, () => {
        router.replace(`/admin/organizations/${created.id}`)
      })
    } else {
      router.replace(`/admin/organizations/${created.id}`)
    }
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedCreate', { resource: t('resources.organization') })))
  } finally {
    isSaving.value = false
  }
}

function handleSuspendClick() {
  if (!org.value) return
  if (org.value.suspended) {
    activateOrg()
  } else {
    suspendDialogOpen.value = true
  }
}

async function suspendOrg() {
  if (!org.value) return
  isSuspending.value = true
  try {
    await organizationsService.suspend(org.value.id)
    toast.success(t('admin.organizations.suspendedSuccess'))
    suspendDialogOpen.value = false
    await loadOrg()
  } catch (e) {
    toast.error(getErrorMessage(e, t('admin.organizations.toggleSuspendFailed')))
  } finally {
    isSuspending.value = false
  }
}

async function activateOrg() {
  if (!org.value) return
  isSuspending.value = true
  try {
    await organizationsService.activate(org.value.id)
    toast.success(t('admin.organizations.activatedSuccess'))
    await loadOrg()
  } catch (e) {
    toast.error(getErrorMessage(e, t('admin.organizations.toggleSuspendFailed')))
  } finally {
    isSuspending.value = false
  }
}

async function deleteOrg() {
  if (!org.value) return
  isDeleting.value = true
  try {
    await organizationsService.delete(org.value.id)
    toast.success(t('common.deletedSuccess', { resource: t('resources.Organization') }))
    router.push('/admin/organizations')
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.failedDelete', { resource: t('resources.organization') })))
  } finally {
    isDeleting.value = false
    deleteDialogOpen.value = false
  }
}

// --- Members ---
const memberColumns = computed<Column<OrganizationMember>[]>(() => [
  { key: 'name', label: t('common.name') },
  { key: 'email', label: t('common.email') },
  { key: 'role', label: t('users.role') },
  { key: 'joined', label: t('admin.organizations.joined') },
  { key: 'actions', label: t('common.actions'), align: 'right' },
])

const addMemberEmail = ref('')
const isAddingMember = ref(false)

async function addExistingMember() {
  if (!org.value) return
  if (!addMemberEmail.value.trim()) {
    toast.error(t('users.enterEmail'))
    return
  }
  isAddingMember.value = true
  try {
    await organizationsService.addMember({ email: addMemberEmail.value.trim() }, org.value.id)
    toast.success(t('users.existingUserAdded'))
    addMemberEmail.value = ''
    await loadMembers()
  } catch (e) {
    toast.error(getErrorMessage(e, t('users.addExistingFailed')))
  } finally {
    isAddingMember.value = false
  }
}

const isResettingPasswordId = ref<string | null>(null)

async function resetMemberPassword(member: OrganizationMember) {
  isResettingPasswordId.value = member.id
  try {
    const response = await usersService.resetPassword(member.user_id)
    const data = (response.data as any).data || response.data
    showPasswordDialog(member.email, data.password)
  } catch (e) {
    toast.error(getErrorMessage(e, t('common.resetPasswordFailed')))
  } finally {
    isResettingPasswordId.value = null
  }
}

const removeMemberDialogOpen = ref(false)
const memberToRemove = ref<OrganizationMember | null>(null)
const isRemovingMember = ref(false)

function openRemoveMemberDialog(member: OrganizationMember) {
  memberToRemove.value = member
  removeMemberDialogOpen.value = true
}

async function confirmRemoveMember() {
  if (!org.value || !memberToRemove.value) return
  isRemovingMember.value = true
  try {
    await organizationsService.removeMember(memberToRemove.value.id, org.value.id)
    toast.success(t('teams.memberRemoved', 'Member removed'))
    removeMemberDialogOpen.value = false
    memberToRemove.value = null
    await loadMembers()
  } catch (e) {
    toast.error(getErrorMessage(e, t('teams.memberRemoveFailed', 'Failed to remove member')))
  } finally {
    isRemovingMember.value = false
  }
}

onMounted(async () => {
  if (isNew.value) {
    isLoading.value = false
    hasChanges.value = false
  } else {
    await loadOrg()
  }
})
</script>

<template>
  <div class="h-full">
    <DetailPageLayout
      :title="isNew ? $t('admin.organizations.newOrganization') : (org?.name || '')"
      :icon="Building2"
      icon-gradient="bg-gradient-to-br from-violet-500 to-purple-600 shadow-violet-500/20"
      back-link="/admin/organizations"
      :breadcrumbs="breadcrumbs"
      :is-loading="isLoading"
      :is-not-found="isNotFound"
      :not-found-title="$t('admin.organizations.notFound')"
    >
      <template #actions>
        <div class="flex items-center gap-2">
          <Button v-if="isNew || hasChanges" size="sm" :disabled="isSaving" @click="isNew ? createOrg() : saveName()">
            <Save class="h-4 w-4 mr-1" /> {{ isSaving ? $t('common.saving', 'Saving...') : isNew ? $t('common.create') : $t('common.save') }}
          </Button>
          <Button v-if="!isNew" variant="outline" size="sm" :disabled="isSuspending" @click="handleSuspendClick">
            <component :is="org?.suspended ? CheckCircle : Ban" class="h-4 w-4 mr-1" />
            {{ org?.suspended ? $t('admin.organizations.activate') : $t('admin.organizations.suspend') }}
          </Button>
          <Button v-if="!isNew" variant="destructive" size="sm" @click="deleteDialogOpen = true">
            <Trash2 class="h-4 w-4 mr-1" /> {{ $t('common.delete') }}
          </Button>
        </div>
      </template>

      <!-- Create mode -->
      <Card v-if="isNew">
        <CardHeader class="pb-3">
          <CardTitle class="text-sm font-medium">{{ $t('admin.organizations.newOrganization') }}</CardTitle>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('admin.organizations.orgName') }} <span class="text-destructive">*</span></Label>
            <Input v-model="createForm.org_name" :placeholder="$t('admin.organizations.orgNamePlaceholder')" />
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('admin.organizations.adminEmail') }} <span class="text-destructive">*</span></Label>
            <Input v-model="createForm.admin_email" type="email" :placeholder="$t('admin.organizations.adminEmailPlaceholder')" />
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">{{ $t('admin.organizations.adminFullName') }} <span class="text-destructive">*</span></Label>
            <Input v-model="createForm.admin_full_name" :placeholder="$t('admin.organizations.adminFullNamePlaceholder')" />
          </div>
          <div class="space-y-2">
            <Label class="text-xs">{{ $t('admin.organizations.adminPassword') }}</Label>
            <RadioGroup v-model="createForm.passwordMode" class="gap-3">
              <div class="flex items-start gap-2">
                <RadioGroupItem id="pw-auto" value="auto" class="mt-0.5" />
                <Label for="pw-auto" class="cursor-pointer font-normal">
                  <span class="block text-sm">{{ $t('admin.organizations.autoGeneratePassword') }}</span>
                  <span class="block text-xs text-muted-foreground">{{ $t('admin.organizations.autoGeneratePasswordDesc') }}</span>
                </Label>
              </div>
              <div class="flex items-start gap-2">
                <RadioGroupItem id="pw-manual" value="manual" class="mt-0.5" />
                <Label for="pw-manual" class="cursor-pointer font-normal">
                  <span class="block text-sm">{{ $t('admin.organizations.setPasswordManually') }}</span>
                </Label>
              </div>
            </RadioGroup>
            <template v-if="createForm.passwordMode === 'manual'">
              <Input v-model="createForm.admin_password" type="password" :placeholder="$t('admin.organizations.passwordPlaceholder')" class="mt-2" />
              <p class="text-xs text-muted-foreground">{{ $t('auth.passwordMinLength') }}</p>
            </template>
          </div>
        </CardContent>
      </Card>

      <!-- Edit mode -->
      <template v-else>
        <Card>
          <CardHeader class="pb-3">
            <div class="flex items-center justify-between">
              <CardTitle class="text-sm font-medium">{{ $t('teams.details', 'Details') }}</CardTitle>
              <Badge :variant="org?.suspended ? 'destructive' : 'default'">
                {{ org?.suspended ? $t('admin.organizations.suspended') : $t('common.active') }}
              </Badge>
            </div>
          </CardHeader>
          <CardContent class="space-y-4">
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('admin.organizations.orgName') }} <span class="text-destructive">*</span></Label>
              <Input v-model="nameForm.name" />
            </div>
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('admin.organizations.slug') }}</Label>
              <Input :model-value="org?.slug || '—'" readonly disabled class="text-muted-foreground" />
            </div>
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('admin.organizations.logo') }}</Label>
              <div class="flex items-center gap-4">
                <div class="h-16 w-16 rounded-lg border bg-muted/30 flex items-center justify-center overflow-hidden shrink-0">
                  <img v-if="org?.logo_data_url" :src="org.logo_data_url" :alt="$t('admin.organizations.logo')" class="h-full w-full object-contain" />
                  <Building2 v-else class="h-6 w-6 text-muted-foreground/40" />
                </div>
                <div class="space-y-1.5">
                  <input ref="logoInput" type="file" accept="image/png,image/jpeg,image/webp,image/svg+xml" class="hidden" @change="uploadOrgLogo" />
                  <div class="flex items-center gap-2">
                    <Button variant="outline" size="sm" @click="logoInput?.click()" :disabled="isUploadingLogo">
                      <Loader2 v-if="isUploadingLogo" class="h-4 w-4 mr-1 animate-spin" />
                      <Upload v-else class="h-4 w-4 mr-1" />
                      {{ org?.logo_data_url ? $t('admin.organizations.changeLogo') : $t('admin.organizations.uploadLogo') }}
                    </Button>
                    <Button v-if="org?.logo_data_url" variant="ghost" size="sm" class="text-muted-foreground" @click="removeOrgLogo" :disabled="isRemovingLogo">
                      {{ $t('common.remove') }}
                    </Button>
                  </div>
                  <p class="text-xs text-muted-foreground">{{ $t('admin.organizations.logoHint') }}</p>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>

        <!-- Roles & Permissions -->
        <Card>
          <CardHeader class="pb-3">
            <CardTitle class="text-sm font-medium">{{ $t('admin.organizations.rolesAndPermissions') }}</CardTitle>
          </CardHeader>
          <CardContent>
            <div class="flex items-center justify-between gap-4">
              <p class="text-sm text-muted-foreground">{{ $t('admin.organizations.rolesAndPermissionsDesc') }}</p>
              <Button variant="outline" size="sm" class="shrink-0" @click="manageRolesAndPermissions">
                <Shield class="h-4 w-4 mr-1" /> {{ $t('admin.organizations.manageRoles') }}
              </Button>
            </div>
          </CardContent>
        </Card>

        <!-- Members -->
        <Card>
          <CardHeader class="pb-3">
            <CardTitle class="text-sm font-medium">{{ $t('admin.organizations.members') }} ({{ members.length }})</CardTitle>
          </CardHeader>
          <CardContent class="space-y-4">
            <div class="space-y-1.5">
              <Label class="text-xs">{{ $t('admin.organizations.addExistingUser') }}</Label>
              <div class="flex gap-2">
                <Input v-model="addMemberEmail" type="email" :placeholder="$t('users.existingEmailPlaceholder')" class="flex-1" />
                <Button variant="outline" size="sm" :disabled="isAddingMember || !addMemberEmail.trim()" @click="addExistingMember">
                  <UserPlus class="h-4 w-4 mr-1" /> {{ $t('common.add') }}
                </Button>
              </div>
              <p class="text-xs text-muted-foreground">{{ $t('admin.organizations.addExistingUserDesc') }}</p>
            </div>

            <DataTable
              :items="members"
              :columns="memberColumns"
              :is-loading="isMembersLoading"
              :empty-title="$t('teams.noMembers', 'No members yet')"
            >
              <template #cell-name="{ item }"><span class="font-medium">{{ item.full_name }}</span></template>
              <template #cell-email="{ item }"><span class="text-muted-foreground">{{ item.email }}</span></template>
              <template #cell-role="{ item }"><Badge variant="outline" class="text-xs">{{ item.role_name || $t('users.noRole') }}</Badge></template>
              <template #cell-joined="{ item }"><span class="text-muted-foreground">{{ formatDate(item.created_at) }}</span></template>
              <template #cell-actions="{ item }">
                <div class="flex items-center justify-end gap-1">
                  <IconButton :icon="KeyRound" :label="$t('common.resetPassword')" class="h-8 w-8" :loading="isResettingPasswordId === item.id" @click="resetMemberPassword(item)" />
                  <IconButton :icon="UserMinus" :label="$t('common.remove')" class="h-8 w-8 text-destructive" @click="openRemoveMemberDialog(item)" />
                </div>
              </template>
            </DataTable>
          </CardContent>
        </Card>
      </template>

      <template v-if="!isNew" #sidebar>
        <MetadataPanel :created-at="org?.created_at" />
      </template>
    </DetailPageLayout>

    <!-- Suspend confirmation -->
    <ConfirmDialog
      v-model:open="suspendDialogOpen"
      :title="$t('admin.organizations.suspendTitle')"
      :description="$t('admin.organizations.suspendDescription')"
      :confirm-label="$t('admin.organizations.suspend')"
      variant="destructive"
      :is-submitting="isSuspending"
      @confirm="suspendOrg"
    />

    <!-- Delete confirmation -->
    <DeleteConfirmDialog
      v-model:open="deleteDialogOpen"
      :title="$t('admin.organizations.deleteOrganization')"
      :item-name="org?.name"
      :is-submitting="isDeleting"
      @confirm="deleteOrg"
    />

    <!-- Remove member confirmation -->
    <AlertDialog v-model:open="removeMemberDialogOpen">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{{ $t('admin.organizations.removeMember') }}</AlertDialogTitle>
          <AlertDialogDescription>
            {{ $t('admin.organizations.removeMemberConfirm', { name: memberToRemove?.full_name || '' }) }}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel :disabled="isRemovingMember">{{ $t('common.cancel') }}</AlertDialogCancel>
          <AlertDialogAction :disabled="isRemovingMember" @click="confirmRemoveMember">{{ $t('common.remove') }}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>

    <!-- One-time password reveal dialog -->
    <Dialog v-model:open="passwordDialogOpen">
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{{ $t('common.passwordGeneratedTitle') }}</DialogTitle>
          <DialogDescription>
            <div class="flex items-center gap-2 text-amber-600 mt-2">
              <AlertTriangle class="h-4 w-4" />
              <span>{{ $t('common.passwordGeneratedWarning') }}</span>
            </div>
          </DialogDescription>
        </DialogHeader>
        <div class="space-y-4 py-4">
          <div class="space-y-2">
            <Label>{{ $t('common.email') }}</Label>
            <Input :model-value="passwordDialogEmail" readonly class="font-mono text-sm" />
          </div>
          <div class="space-y-2">
            <Label>{{ $t('common.temporaryPassword') }}</Label>
            <div class="flex gap-2">
              <Input :model-value="passwordDialogPassword" readonly class="font-mono text-sm" />
              <IconButton :icon="Copy" :label="$t('common.copy')" variant="outline" @click="copyToClipboard(passwordDialogPassword)" />
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button size="sm" @click="closePasswordDialog">{{ $t('common.done') }}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <UnsavedChangesDialog :open="showLeaveDialog" @stay="cancelLeave" @leave="confirmLeave" />
  </div>
</template>
