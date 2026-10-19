<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '@/services/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toast } from 'vue-sonner'
import { getErrorMessage } from '@/lib/api-utils'
import { KeyRound, Loader2, CheckCircle2 } from 'lucide-vue-next'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()

const token = computed(() => (route.query.token as string) || '')
const password = ref('')
const confirmPassword = ref('')
const isLoading = ref(false)
const isDone = ref(false)

async function handleSubmit() {
  if (!token.value) {
    toast.error(t('auth.setPasswordMissingToken', 'This link is invalid or incomplete.'))
    return
  }
  if (password.value.length < 8) {
    toast.error(t('auth.passwordTooShort', 'Password must be at least 8 characters'))
    return
  }
  if (password.value !== confirmPassword.value) {
    toast.error(t('auth.passwordsDontMatch', 'Passwords do not match'))
    return
  }

  isLoading.value = true
  try {
    await api.post('/auth/set-password', { token: token.value, password: password.value })
    isDone.value = true
    setTimeout(() => router.push('/login'), 2000)
  } catch (e) {
    toast.error(getErrorMessage(e, t('auth.setPasswordFailed', 'This link may have expired or already been used.')))
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-[#0a0a0b] light:bg-gradient-to-br light:from-gray-50 light:to-gray-100 p-4">
    <div class="w-full max-w-md rounded-2xl border border-white/[0.08] bg-white/[0.02] backdrop-blur light:bg-white light:border-gray-200 light:shadow-xl">
      <div class="p-8 space-y-1 text-center">
        <div class="flex justify-center mb-4">
          <div class="h-12 w-12 rounded-xl bg-gradient-to-br from-emerald-500 to-green-600 flex items-center justify-center shadow-lg shadow-emerald-500/20">
            <KeyRound class="h-7 w-7 text-white" />
          </div>
        </div>
        <h2 class="text-2xl font-bold text-white light:text-gray-900">{{ $t('auth.setPasswordTitle', 'Set your password') }}</h2>
        <p class="text-white/50 light:text-gray-500">{{ $t('auth.setPasswordSubtitle', 'Choose a password to activate your account.') }}</p>
      </div>

      <div v-if="isDone" class="px-8 pb-8 text-center space-y-3">
        <CheckCircle2 class="h-10 w-10 mx-auto text-emerald-500" />
        <p class="text-white/70 light:text-gray-700">{{ $t('auth.setPasswordSuccess', "Password set! Taking you to login...") }}</p>
      </div>

      <form v-else @submit.prevent="handleSubmit">
        <div class="px-8 pb-4 space-y-4">
          <div class="space-y-2">
            <Label for="new-password" class="text-white/70 light:text-gray-700">{{ $t('auth.newPassword', 'New Password') }}</Label>
            <Input id="new-password" v-model="password" type="password" :disabled="isLoading" autocomplete="new-password" />
          </div>
          <div class="space-y-2">
            <Label for="confirm-password" class="text-white/70 light:text-gray-700">{{ $t('auth.confirmPassword', 'Confirm Password') }}</Label>
            <Input id="confirm-password" v-model="confirmPassword" type="password" :disabled="isLoading" autocomplete="new-password" />
            <p class="text-xs text-white/40 light:text-gray-500">{{ $t('auth.passwordMinLength', 'At least 8 characters') }}</p>
          </div>
          <Button type="submit" class="w-full" :disabled="isLoading">
            <Loader2 v-if="isLoading" class="mr-2 h-4 w-4 animate-spin" />
            {{ $t('auth.setPasswordSubmit', 'Set Password & Continue') }}
          </Button>
        </div>
      </form>
    </div>
  </div>
</template>
