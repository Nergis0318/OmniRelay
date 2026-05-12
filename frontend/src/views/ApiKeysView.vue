<template>
  <div>
    <div class="d-flex align-center mb-6">
      <h1 class="text-h4">API Keys</h1>
      <v-spacer />
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openCreateDialog">
        Issue Key
      </v-btn>
    </div>

    <v-card>
      <v-data-table :headers="headers" :items="store.apiKeys" :loading="store.loading" hover>
        <template #item.key_prefix="{ item }">
          <code>{{ item.key_prefix }}</code>
        </template>
        <template #item.is_active="{ item }">
          <v-chip :color="item.is_active ? 'success' : 'error'" size="small" variant="tonal">
            {{ item.is_active ? 'Active' : 'Inactive' }}
          </v-chip>
        </template>
        <template #item.last_used_at="{ item }">
          {{ item.last_used_at ? new Date(item.last_used_at).toLocaleString() : 'Never' }}
        </template>
        <template #item.actions="{ item }">
          <v-icon v-if="item.is_active" size="small" color="error" @click="handleDelete(item.id)" title="Deactivate">
            mdi-block-helper
          </v-icon>
        </template>
      </v-data-table>
    </v-card>

    <v-dialog v-model="createDialog" max-width="500">
      <v-card>
        <v-card-title>Issue New API Key</v-card-title>
        <v-card-text>
          <v-text-field v-model="form.name" label="Key Name" placeholder="My App Key" />
          <v-text-field v-model.number="form.rate_limit_rpm" label="Rate Limit (RPM, 0=unlimited)" type="number" />
          <v-alert v-if="dialogError" type="error" density="compact" class="mt-2">
            {{ dialogError }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="createDialog = false">Cancel</v-btn>
          <v-btn color="primary" @click="handleCreate" :loading="creating">Create</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="showKey" max-width="550">
      <v-card>
        <v-card-title class="text-success">API Key Created</v-card-title>
        <v-card-text>
          <p class="text-caption mb-2">Copy this key now. You won't be able to see it again.</p>
          <v-text-field
            :model-value="newKey"
            readonly
            append-inner-icon="mdi-content-copy"
            @click:append-inner="copyKey"
            density="compact"
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn color="primary" @click="showKey = false; createDialog = false">Done</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useApiKeysStore } from '../stores/apikeys'

const store = useApiKeysStore()
const createDialog = ref(false)
const showKey = ref(false)
const newKey = ref('')
const creating = ref(false)
const dialogError = ref('')

const form = ref({
  name: '',
  rate_limit_rpm: 0,
})

const headers = [
  { title: 'Name', key: 'name' },
  { title: 'Key Prefix', key: 'key_prefix' },
  { title: 'Status', key: 'is_active' },
  { title: 'Rate Limit', key: 'rate_limit_rpm' },
  { title: 'Last Used', key: 'last_used_at' },
  { title: 'Created', key: 'created_at' },
  { title: 'Actions', key: 'actions', sortable: false, width: 80 },
]

function openCreateDialog() {
  dialogError.value = ''
  form.value = { name: '', rate_limit_rpm: 0 }
  createDialog.value = true
}

async function handleCreate() {
  creating.value = true
  dialogError.value = ''
  try {
    const { data } = await store.create(form.value.name, form.value.rate_limit_rpm)
    newKey.value = data.plain_key
    showKey.value = true
    await store.fetch()
  } catch (e: any) {
    dialogError.value = e.response?.data?.error || 'Failed to create key'
  } finally {
    creating.value = false
  }
}

function copyKey() {
  navigator.clipboard.writeText(newKey.value)
}

async function handleDelete(id: number) {
  if (!confirm('Deactivate this API key?')) return
  await store.remove(id)
}

onMounted(() => store.fetch())
</script>
