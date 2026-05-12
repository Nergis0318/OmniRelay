<template>
  <div>
    <div class="d-flex align-center mb-6">
      <h1 class="text-h4">Providers</h1>
      <v-spacer />
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openDialog()">
        Add Provider
      </v-btn>
    </div>

    <v-card>
      <v-data-table :headers="headers" :items="store.providers" :loading="store.loading" hover>
        <template #item.is_active="{ item }">
          <v-chip :color="item.is_active ? 'success' : 'error'" size="small" variant="tonal">
            {{ item.is_active ? 'Active' : 'Inactive' }}
          </v-chip>
        </template>
        <template #item.provider_type="{ item }">
          <v-chip size="small" variant="outlined">{{ item.provider_type }}</v-chip>
        </template>
        <template #item.actions="{ item }">
          <v-icon size="small" class="me-2" @click="openDialog(item)" title="Edit">mdi-pencil</v-icon>
          <v-icon size="small" class="me-2" @click="handleSync(item.id)" title="Sync Models">mdi-sync</v-icon>
          <v-icon size="small" color="error" @click="handleDelete(item.id)" title="Delete">mdi-delete</v-icon>
        </template>
      </v-data-table>
    </v-card>

    <v-dialog v-model="dialog" max-width="560">
      <v-card>
        <v-card-title>{{ editing ? 'Edit Provider' : 'Add Provider' }}</v-card-title>
        <v-card-text>
          <v-text-field v-model="form.provider_key" label="Provider Key" placeholder="e.g. openai, my-llama" :disabled="!!editing" />
          <v-text-field v-model="form.name" label="Display Name" placeholder="e.g. OpenAI, My LLM Server" />
          <v-text-field v-model="form.api_base_url" label="API Base URL" placeholder="https://api.openai.com/v1" />
          <v-text-field v-model="form.api_key" label="API Key" type="password" :placeholder="editing ? '(leave empty to keep)' : 'sk-...'" />

          <v-select
            v-model="form.provider_type"
            :items="['openai', 'anthropic', 'lmstudio', 'ollama', 'gemini']"
            label="Provider Type"
          />

          <v-checkbox v-model="form.auto_sync" label="Auto-fetch model list from provider" hint="After save, automatically pull available models from the provider's API" persistent-hint />

          <v-alert v-if="syncResult" type="success" density="compact" class="mt-2" variant="tonal">
            {{ syncResult }}
          </v-alert>
          <v-alert v-if="dialogError" type="error" density="compact" class="mt-2">
            {{ dialogError }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="dialog = false">Cancel</v-btn>
          <v-btn color="primary" @click="handleSave" :loading="saving">
            {{ editing ? 'Update' : 'Create' }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useProvidersStore } from '../stores/providers'

const store = useProvidersStore()
const dialog = ref(false)
const editing = ref<any>(null)
const saving = ref(false)
const dialogError = ref('')
const syncResult = ref('')

const form = ref({
  provider_key: '',
  name: '',
  api_base_url: '',
  api_key: '',
  provider_type: 'openai',
  auto_sync: true,
})

const headers = [
  { title: 'Key', key: 'provider_key', sortable: true },
  { title: 'Name', key: 'name', sortable: true },
  { title: 'Type', key: 'provider_type' },
  { title: 'Status', key: 'is_active' },
  { title: 'Actions', key: 'actions', sortable: false },
]

function openDialog(provider?: any) {
  dialogError.value = ''
  syncResult.value = ''
  if (provider) {
    editing.value = provider
    form.value = { ...provider, api_key: '', auto_sync: false }
  } else {
    editing.value = null
    form.value = { provider_key: '', name: '', api_base_url: '', api_key: '', provider_type: 'openai', auto_sync: true }
  }
  dialog.value = true
}

async function handleSave() {
  saving.value = true
  dialogError.value = ''
  syncResult.value = ''
  try {
    if (editing.value) {
      await store.update(editing.value.id, form.value)
      if (form.value.auto_sync) {
        const { data } = await store.syncModels(editing.value.id)
        syncResult.value = `Synced ${data.model_count} models`
      }
    } else {
      const created = await store.create(form.value)
      if (form.value.auto_sync) {
        const { data } = await store.syncModels(created.id)
        syncResult.value = `Synced ${data.model_count} models`
      }
    }
    dialog.value = false
  } catch (e: any) {
    dialogError.value = e.response?.data?.error || 'Failed to save'
  } finally {
    saving.value = false
  }
}

async function handleSync(id: number) {
  syncResult.value = ''
  try {
    const { data } = await store.syncModels(id)
    syncResult.value = `Synced ${data.model_count} models`
  } catch (e: any) {
    dialogError.value = e.response?.data?.error || 'Sync failed'
  }
}

async function handleDelete(id: number) {
  if (!confirm('Delete this provider and all its models?')) return
  await store.remove(id)
}

onMounted(() => store.fetch())
</script>
