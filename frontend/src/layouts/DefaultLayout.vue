<template>
  <v-navigation-drawer v-model="drawer" :rail="rail" permanent>
    <v-list-item
      prepend-avatar="https://api.dicebear.com/9.x/initials/svg?seed=OR"
      :title="auth.user?.username || 'Admin'"
      subtitle="OmniRelay Gateway"
      nav
    >
      <template #append>
        <v-btn
          :icon="rail ? 'mdi-chevron-right' : 'mdi-chevron-left'"
          variant="text"
          @click.stop="rail = !rail"
        />
      </template>
    </v-list-item>

    <v-divider />

    <v-list nav density="compact">
      <v-list-item
        v-for="item in menuItems"
        :key="item.title"
        :prepend-icon="item.icon"
        :title="item.title"
        :to="item.to"
      />
    </v-list>

    <template #append>
      <div class="pa-2">
        <v-btn
          v-if="!rail"
          block
          color="error"
          variant="text"
          prepend-icon="mdi-logout"
          @click="handleLogout"
        >
          Logout
        </v-btn>
        <v-btn v-else icon="mdi-logout" color="error" variant="text" @click="handleLogout" />
      </div>
    </template>
  </v-navigation-drawer>

  <v-main>
    <v-container fluid class="pa-6">
      <router-view />
    </v-container>
  </v-main>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const router = useRouter()
const drawer = ref(true)
const rail = ref(false)

const menuItems = [
  { title: 'Dashboard', icon: 'mdi-view-dashboard', to: '/' },
  { title: 'Providers', icon: 'mdi-server', to: '/providers' },
  { title: 'Models', icon: 'mdi-cube-outline', to: '/models' },
  { title: 'API Keys', icon: 'mdi-key-variant', to: '/api-keys' },
  { title: 'Usage', icon: 'mdi-chart-line', to: '/usage' },
]

function handleLogout() {
  auth.logout()
  router.push('/login')
}
</script>
