import { defineStore } from 'pinia'
import { ref } from 'vue'
import api from '../api/client'

interface APIKey {
  id: number
  key_prefix: string
  name: string
  is_active: boolean
  rate_limit_rpm: number
  created_at: string
  last_used_at: string | null
}

export const useApiKeysStore = defineStore('apikeys', () => {
  const apiKeys = ref<APIKey[]>([])
  const loading = ref(false)

  async function fetch() {
    loading.value = true
    try {
      const { data } = await api.get('/api-keys')
      apiKeys.value = data.api_keys
    } finally {
      loading.value = false
    }
  }

  async function create(name: string, rateLimitRpm: number) {
    return await api.post('/api-keys', { name, rate_limit_rpm: rateLimitRpm })
  }

  async function remove(id: number) {
    await api.delete(`/api-keys/${id}`)
    await fetch()
  }

  return { apiKeys, loading, fetch, create, remove }
})
