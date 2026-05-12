<template>
  <v-container class="fill-height" fluid>
    <v-row align="center" justify="center">
      <v-col cols="12" sm="8" md="4">
        <v-card>
          <v-card-title class="text-h5 text-center pa-4">Create Account</v-card-title>
          <v-card-text>
            <v-form @submit.prevent="handleRegister">
              <v-text-field
                v-model="username"
                label="Username"
                prepend-icon="mdi-account"
                required
              />
              <v-text-field
                v-model="password"
                label="Password"
                type="password"
                prepend-icon="mdi-lock"
                required
              />
              <v-text-field
                v-model="confirmPassword"
                label="Confirm Password"
                type="password"
                prepend-icon="mdi-lock-check"
                required
              />
              <v-alert v-if="error" type="error" density="compact" class="mb-3">
                {{ error }}
              </v-alert>
              <v-btn type="submit" block color="primary" :loading="loading" :disabled="password !== confirmPassword">
                Register
              </v-btn>
            </v-form>
          </v-card-text>
          <v-card-actions class="justify-center pb-4">
            <span class="text-caption">
              Already have an account?
              <router-link to="/login">Sign In</router-link>
            </span>
          </v-card-actions>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const router = useRouter()
const username = ref('')
const password = ref('')
const confirmPassword = ref('')
const error = ref('')
const loading = ref(false)

async function handleRegister() {
  if (password.value !== confirmPassword.value) return
  loading.value = true
  error.value = ''
  try {
    await auth.register(username.value, password.value)
    router.push('/')
  } catch (e: any) {
    error.value = e.response?.data?.error || 'Registration failed'
  } finally {
    loading.value = false
  }
}
</script>
