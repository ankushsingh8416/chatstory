<script setup lang="ts">
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  MessageSquare,
  ChevronLeft,
  ChevronRight,
  Menu,
  X
} from 'lucide-vue-next'
import { wsService } from '@/services/websocket'
import { authService } from '@/services/api'
import OrganizationSwitcher from './OrganizationSwitcher.vue'
import UserMenu from './UserMenu.vue'
import ActiveCallPanel from '@/components/calling/ActiveCallPanel.vue'
import { ScrollToTop } from '@/components/shared'
import { navigationSections, type NavSection } from './navigation'

useI18n() // Enable $t() in template

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const isCollapsed = ref(true)
const isMobileMenuOpen = ref(false)

// Refresh user data and connect WebSocket on mount
onMounted(() => {
  if (authStore.isAuthenticated) {
    // Fetch fresh permissions in background (non-destructive — interceptor handles 401)
    authStore.refreshUserData()

    wsService.connect(async () => {
      try {
        const resp = await authService.getWSToken()
        return resp.data.data.token
      } catch {
        return null
      }
    })
  }
})

function filterItems(items: NavSection['items']) {
  return items
    .filter(item => {
      if (item.superAdminOnly && !authStore.user?.is_super_admin) {
        return false
      }
      if (item.childPermissions) {
        return item.childPermissions.some(p => authStore.hasPermission(p, 'read'))
      }
      return !item.permission || authStore.hasPermission(item.permission, 'read')
    })
    .map(item => {
      const filteredChildren = item.children?.filter(
        child => (!child.superAdminOnly || authStore.user?.is_super_admin) &&
          (!child.permission || authStore.hasPermission(child.permission, 'read'))
      )

      let effectivePath = item.path
      if (item.childPermissions && item.permission && !authStore.hasPermission(item.permission, 'read') && filteredChildren?.length) {
        effectivePath = filteredChildren[0].path
      }

      const originalPath = item.path
      const isActive = originalPath === '/'
        ? route.name === 'dashboard'
        : originalPath === '/chat'
          ? route.name === 'chat' || route.name === 'chat-conversation'
          : route.path.startsWith(originalPath)

      return {
        ...item,
        path: effectivePath,
        active: isActive,
        children: filteredChildren
      }
    })
}

// Filter navigation sections based on user permissions
const navSections = computed(() => {
  return navigationSections
    .map(section => ({
      ...section,
      items: filterItems(section.items)
    }))
    .filter(section => section.items.length > 0)
})

const mainSections = computed(() => navSections.value.filter(s => !s.pinBottom))
const bottomSections = computed(() => navSections.value.filter(s => s.pinBottom))

// Which parent item's submenu is currently expanded, keyed by item.name
// (a stable translation-key identifier — item.path can be swapped to a
// child's path by filterItems, so it's not a safe key here). Independent of
// route-active state so a user can manually collapse a submenu while still
// on one of its pages, and it re-syncs to whichever parent is active when
// navigation happens some other way (e.g. a direct link, browser back).
const openMenuName = ref<string | null>(null)

function findActiveParentName(): string | null {
  const allItems = [...mainSections.value, ...bottomSections.value].flatMap(s => s.items)
  return allItems.find(i => i.children?.length && i.active)?.name ?? null
}

watch(() => route.path, () => {
  const activeParent = findActiveParentName()
  if (activeParent) openMenuName.value = activeParent
}, { immediate: true })

function toggleMenu(item: { name: string; active: boolean; children?: unknown[] }, event: Event) {
  if (!item.children?.length) return

  if (openMenuName.value === item.name) {
    // Already open — close it. If we're already on this item's page, there's
    // nothing to navigate to, so suppress the RouterLink navigation too.
    openMenuName.value = null
    if (item.active) event.preventDefault()
    return
  }

  openMenuName.value = item.name
  // Let the click's own navigation happen, then bring the newly-expanded
  // section (and as much of its children as fits) into view.
  nextTick(() => {
    (event.currentTarget as HTMLElement | null)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  })
}

const toggleSidebar = () => {
  isCollapsed.value = !isCollapsed.value
}

const handleLogout = async () => {
  await authStore.logout()
  router.push('/login')
}
</script>

<template>
  <div class="flex h-screen bg-[#0a0a0b] light:bg-gray-50">
    <!-- Skip link for accessibility -->
    <a href="#main-content" class="skip-link">{{ $t('nav.skipToMain') }}</a>

    <!-- Mobile header -->
    <header class="fixed top-0 left-0 right-0 z-50 flex h-12 items-center justify-between border-b border-white/10 bg-[#0f3d33]/95 backdrop-blur-sm px-3 md:hidden">
      <RouterLink to="/" class="flex items-center gap-2">
        <div class="h-9 w-9 rounded-lg bg-gradient-to-br from-emerald-500 to-green-600 flex items-center justify-center shadow-lg shadow-emerald-500/20">
          <MessageSquare class="h-5 w-5 text-white" />
        </div>
        <span class="font-semibold text-sm text-white">Whank</span>
      </RouterLink>
      <Button
        variant="ghost"
        size="icon"
        class="h-10 w-10 text-white/70 hover:text-white hover:bg-white/[0.08]"
        aria-label="Toggle menu"
        :aria-expanded="isMobileMenuOpen"
        @click="isMobileMenuOpen = !isMobileMenuOpen"
      >
        <X v-if="isMobileMenuOpen" class="h-7 w-7" />
        <Menu v-else class="h-7 w-7" />
      </Button>
    </header>

    <!-- Mobile menu overlay -->
    <div
      v-if="isMobileMenuOpen"
      class="fixed inset-0 z-40 bg-black/60 light:bg-black/30 backdrop-blur-sm md:hidden"
      @click="isMobileMenuOpen = false"
    />

    <!-- Sidebar -->
    <aside
      :class="[
        'flex flex-col border-r border-black/20 bg-[#0f3d33] transition-all duration-300',
        'fixed inset-y-0 left-0 z-40 md:relative',
        'transform md:transform-none',
        isMobileMenuOpen ? 'translate-x-0' : '-translate-x-full md:translate-x-0',
        isCollapsed ? 'w-64 md:w-16' : 'w-64'
      ]"
      role="navigation"
      aria-label="Main navigation"
    >
      <!-- Logo (hidden on mobile, shown in header instead) -->
      <div
        class="hidden md:flex h-12 items-center px-3 border-b border-white/10"
        :class="isCollapsed ? 'justify-center' : 'justify-between'"
      >
        <RouterLink to="/" class="flex items-center gap-2">
          <div class="h-9 w-9 rounded-lg bg-gradient-to-br from-emerald-500 to-green-600 flex items-center justify-center shadow-lg shadow-emerald-500/20 shrink-0">
            <MessageSquare class="h-5 w-5 text-white" />
          </div>
          <span
            v-if="!isCollapsed"
            class="font-semibold text-sm text-white"
          >
            Whank
          </span>
        </RouterLink>
        <!-- Collapse toggle: inline here only while expanded, where there's
             room next to the logo. While collapsed it moves to a floating
             edge button below (see outside this row) so it never has to
             squeeze into the narrow 64px collapsed column alongside the logo. -->
        <Button
          v-if="!isCollapsed"
          variant="ghost"
          size="icon"
          class="h-9 w-9 text-white/80 hover:text-white hover:bg-white/[0.08] shrink-0"
          :aria-label="$t('nav.collapseSidebar')"
          :aria-expanded="true"
          @click="toggleSidebar"
        >
          <ChevronLeft class="h-5 w-5" />
        </Button>
      </div>
      <!-- Mobile logo spacer -->
      <div class="h-12 md:hidden" />

      <!-- Floating expand toggle — only rendered while collapsed, anchored to
           the sidebar's own edge so it always has its own space and is never
           hidden or squeezed by other header content. -->
      <Button
        v-if="isCollapsed"
        variant="ghost"
        size="icon"
        class="hidden md:flex absolute top-[26px] -right-3 h-6 w-6 rounded-full border border-black/20 bg-[#0f3d33] text-white/80 hover:text-white hover:bg-[#15493f] shadow-md z-10"
        :aria-label="$t('nav.expandSidebar')"
        aria-expanded="false"
        @click="toggleSidebar"
      >
        <ChevronRight class="h-3.5 w-3.5" />
      </Button>

      <!-- Organization Switcher (Super Admin only) -->
      <OrganizationSwitcher :collapsed="isCollapsed" />

      <!-- Navigation -->
      <ScrollArea class="flex-1 py-2">
        <nav class="px-2" role="menubar">
          <template v-for="(section, sIdx) in mainSections" :key="section.label">
            <!-- Section header -->
            <div
              v-if="section.label && !isCollapsed"
              :class="['px-2.5 pt-4 pb-1 text-[10px] font-semibold uppercase tracking-wider text-white/75', sIdx === 0 && 'pt-1']"
            >
              {{ $t(section.label) }}
            </div>
            <div v-else-if="sIdx > 0" :class="['my-2 mx-2.5 border-t border-white/10', isCollapsed && 'mx-1']" />

            <!-- Section items -->
            <div class="space-y-0.5">
              <template v-for="item in section.items" :key="item.path">
                <RouterLink
                  :to="item.path"
                  :class="[
                    'nav-active-indicator btn-press flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-all duration-200',
                    item.active
                      ? 'bg-white/[0.12] text-white'
                      : 'text-white/90 hover:text-white hover:bg-white/[0.08]',
                    isCollapsed && 'md:justify-center md:px-2'
                  ]"
                  :data-active="item.active"
                  role="menuitem"
                  :aria-current="item.active ? 'page' : undefined"
                  :aria-expanded="item.children?.length ? openMenuName === item.name : undefined"
                  @click="isMobileMenuOpen = false; toggleMenu(item, $event)"
                >
                  <component :is="item.icon" class="h-6 w-6 shrink-0" aria-hidden="true" />
                  <span :class="[isCollapsed && 'md:sr-only', 'flex-1']">{{ $t(item.name) }}</span>
                  <ChevronRight
                    v-if="item.children?.length && !isCollapsed"
                    class="h-4 w-4 shrink-0 text-white/50 transition-transform duration-200"
                    :class="openMenuName === item.name && 'rotate-90'"
                    aria-hidden="true"
                  />
                </RouterLink>

                <!-- Submenu items -->
                <template v-if="item.children?.length && openMenuName === item.name && !isCollapsed">
                  <div class="space-y-0.5 mt-0.5">
                    <RouterLink
                      v-for="child in item.children"
                      :key="child.path"
                      :to="child.path"
                      :class="[
                        'flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm font-medium transition-all duration-200 ml-4',
                        route.path === child.path
                          ? 'bg-white/[0.1] text-white'
                          : 'text-white/85 hover:text-white hover:bg-white/[0.06]'
                      ]"
                      role="menuitem"
                      :aria-current="route.path === child.path ? 'page' : undefined"
                      @click="isMobileMenuOpen = false"
                    >
                      <component :is="child.icon" class="h-5 w-5 shrink-0" aria-hidden="true" />
                      <span>{{ $t(child.name) }}</span>
                    </RouterLink>
                  </div>
                </template>
              </template>
            </div>
          </template>

          <!-- Bottom-pinned navigation (Settings). Kept inside the same
               scroll area as the main nav (rather than a separate fixed
               block below it) so a long Settings submenu scrolls with the
               rest of the sidebar instead of overflowing past the viewport
               and pushing the user menu off-screen. -->
          <div v-if="bottomSections.length > 0" class="mt-2 pt-2 border-t border-white/10 space-y-0.5">
            <template v-for="section in bottomSections" :key="section.label">
              <template v-for="item in section.items" :key="item.path">
                <RouterLink
                  :to="item.path"
                  :class="[
                    'nav-active-indicator btn-press flex items-center gap-2.5 rounded-lg px-2.5 py-2 text-[13px] font-medium transition-all duration-200',
                    item.active
                      ? 'bg-white/[0.12] text-white'
                      : 'text-white/90 hover:text-white hover:bg-white/[0.08]',
                    isCollapsed && 'md:justify-center md:px-2'
                  ]"
                  :data-active="item.active"
                  role="menuitem"
                  :aria-current="item.active ? 'page' : undefined"
                  :aria-expanded="item.children?.length ? openMenuName === item.name : undefined"
                  @click="isMobileMenuOpen = false; toggleMenu(item, $event)"
                >
                  <component :is="item.icon" class="h-6 w-6 shrink-0" aria-hidden="true" />
                  <span :class="[isCollapsed && 'md:sr-only', 'flex-1']">{{ $t(item.name) }}</span>
                  <ChevronRight
                    v-if="item.children?.length && !isCollapsed"
                    class="h-4 w-4 shrink-0 text-white/50 transition-transform duration-200"
                    :class="openMenuName === item.name && 'rotate-90'"
                    aria-hidden="true"
                  />
                </RouterLink>

                <template v-if="item.children?.length && openMenuName === item.name && !isCollapsed">
                  <div class="space-y-0.5 mt-0.5">
                    <RouterLink
                      v-for="child in item.children"
                      :key="child.path"
                      :to="child.path"
                      :class="[
                        'flex items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-[13px] font-medium transition-all duration-200 ml-4',
                        route.path === child.path
                          ? 'bg-white/[0.1] text-white'
                          : 'text-white/85 hover:text-white hover:bg-white/[0.06]'
                      ]"
                      role="menuitem"
                      :aria-current="route.path === child.path ? 'page' : undefined"
                      @click="isMobileMenuOpen = false"
                    >
                      <component :is="child.icon" class="h-5 w-5 shrink-0" aria-hidden="true" />
                      <span>{{ $t(child.name) }}</span>
                    </RouterLink>
                  </div>
                </template>
              </template>
            </template>
          </div>
        </nav>
      </ScrollArea>

      <!-- User Menu -->
      <UserMenu :collapsed="isCollapsed" @logout="handleLogout" />
    </aside>

    <!-- Main content -->
    <main id="main-content" class="flex-1 overflow-hidden pt-12 md:pt-0 bg-[#0a0a0b] light:bg-gray-50" role="main">
      <RouterView v-slot="{ Component, route: viewRoute }">
        <Transition name="page" mode="out-in">
          <component :is="Component" :key="viewRoute.meta.stableKey ? String(viewRoute.name) : viewRoute.path" />
        </Transition>
      </RouterView>
      <ActiveCallPanel />
      <ScrollToTop />
    </main>
  </div>
</template>
