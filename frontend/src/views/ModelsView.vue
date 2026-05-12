<template>
  <div>
    <div class="d-flex align-center mb-6">
      <h1 class="text-h4">Models</h1>
      <v-spacer />
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openDialog()">
        Add Model
      </v-btn>
    </div>

    <v-card>
      <v-data-table :headers="headers" :items="store.models" :loading="store.loading" hover>
        <template #item.full_id="{ item }">
          <code>{{ item.provider_key }}/{{ item.model_id }}</code>
        </template>
        <template #item.is_manual="{ item }">
          <v-chip :color="item.is_manual ? 'warning' : 'success'" size="small" variant="tonal">
            {{ item.is_manual ? 'Manual' : 'Auto' }}
          </v-chip>
        </template>
        <template #item.pricing="{ item }">
          <div class="text-caption">
            In: ${{ item.input_price_per_1mtok }}/1M &nbsp; Out: ${{ item.output_price_per_1mtok }}/1M
            <span v-if="item.cache_write_5m_price_per_1mtok || item.cache_write_1h_price_per_1mtok || item.cache_read_price_per_1mtok">
              <br />CW5m: ${{ item.cache_write_5m_price_per_1mtok }}/1M &nbsp; CW1h: ${{ item.cache_write_1h_price_per_1mtok }}/1M &nbsp; Read: ${{ item.cache_read_price_per_1mtok }}/1M
            </span>
          </div>
        </template>
        <template #item.actions="{ item }">
          <v-icon size="small" class="me-2" @click="openEditDialog(item)" title="Edit">mdi-pencil</v-icon>
          <v-icon size="small" color="error" @click="handleDelete(item.id)" title="Delete">mdi-delete</v-icon>
        </template>
      </v-data-table>
    </v-card>

    <v-dialog v-model="dialog" max-width="520">
      <v-card>
        <v-card-title>
          {{ editMode ? 'Edit Model' : 'Add Model Manually' }}
        </v-card-title>
        <v-card-text>
          <v-select
            v-if="!editMode"
            v-model="form.provider_id"
            :items="providerOptions"
            item-title="text"
            item-value="value"
            label="Provider"
          />
          <v-text-field v-model="form.model_id" label="Model ID" placeholder="gpt-4o" />
          <v-text-field v-model="form.display_name" label="Display Name" placeholder="GPT-4 Omni" />
          <v-text-field v-model.number="form.input_price_per_1mtok" label="Input Price ($/1M tokens)" type="number" step="0.01" />
          <v-text-field v-model.number="form.output_price_per_1mtok" label="Output Price ($/1M tokens)" type="number" step="0.01" />
          <v-text-field v-model.number="form.cache_write_5m_price_per_1mtok" label="Cache Write 5m Price ($/1M tokens)" type="number" step="0.01" hint="5-minute cache writes" persistent-hint />
          <v-text-field v-model.number="form.cache_write_1h_price_per_1mtok" label="Cache Write 1h Price ($/1M tokens)" type="number" step="0.01" hint="1-hour cache writes" persistent-hint />
          <v-text-field v-model.number="form.cache_read_price_per_1mtok" label="Cache Read Price ($/1M tokens)" type="number" step="0.01" hint="Cache reads &amp; refreshes" persistent-hint />
          <v-text-field v-model.number="form.context_window" label="Context Window" type="number" />
          <v-alert v-if="dialogError" type="error" density="compact" class="mt-2">
            {{ dialogError }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="dialog = false">Cancel</v-btn>
          <v-btn color="primary" @click="handleSave" :loading="saving">
            {{ editMode ? 'Update' : 'Create' }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useModelsStore } from '../stores/models'
import { useProvidersStore } from '../stores/providers'

const store = useModelsStore()
const providersStore = useProvidersStore()
const dialog = ref(false)
const editMode = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const dialogError = ref('')

const form = ref({
  provider_id: null as number | null,
  model_id: '',
  display_name: '',
  input_price_per_1mtok: 0,
  output_price_per_1mtok: 0,
  cache_write_5m_price_per_1mtok: 0,
  cache_write_1h_price_per_1mtok: 0,
  cache_read_price_per_1mtok: 0,
  context_window: 0,
})

const providerOptions = computed(() =>
  providersStore.providers.map((p) => ({ text: p.name, value: p.id }))
)

const headers = [
  { title: 'Full ID', key: 'full_id', sortable: false },
  { title: 'Provider', key: 'provider_key' },
  { title: 'Model ID', key: 'model_id' },
  { title: 'Source', key: 'is_manual' },
  { title: 'Pricing ($/1M)', key: 'pricing', sortable: false },
  { title: 'Actions', key: 'actions', sortable: false, width: 100 },
]

function openDialog() {
  editMode.value = false
  editingId.value = null
  dialogError.value = ''
  form.value = {
    provider_id: providersStore.providers[0]?.id || null,
    model_id: '',
    display_name: '',
    input_price_per_1mtok: 0,
    output_price_per_1mtok: 0,
    cache_write_5m_price_per_1mtok: 0,
    cache_write_1h_price_per_1mtok: 0,
    cache_read_price_per_1mtok: 0,
    context_window: 0,
  }
  dialog.value = true
}

function openEditDialog(model: any) {
  editMode.value = true
  editingId.value = model.id
  dialogError.value = ''
  form.value = {
    provider_id: model.provider_id,
    model_id: model.model_id,
    display_name: model.display_name,
    input_price_per_1mtok: model.input_price_per_1mtok || 0,
    output_price_per_1mtok: model.output_price_per_1mtok || 0,
    cache_write_5m_price_per_1mtok: model.cache_write_5m_price_per_1mtok || 0,
    cache_write_1h_price_per_1mtok: model.cache_write_1h_price_per_1mtok || 0,
    cache_read_price_per_1mtok: model.cache_read_price_per_1mtok || 0,
    context_window: model.context_window || 0,
  }
  dialog.value = true
}

async function handleSave() {
  saving.value = true
  dialogError.value = ''
  try {
    if (editMode.value && editingId.value) {
      await store.update(editingId.value, {
        display_name: form.value.display_name,
        input_price_per_1mtok: form.value.input_price_per_1mtok,
        output_price_per_1mtok: form.value.output_price_per_1mtok,
        cache_write_5m_price_per_1mtok: form.value.cache_write_5m_price_per_1mtok,
        cache_write_1h_price_per_1mtok: form.value.cache_write_1h_price_per_1mtok,
        cache_read_price_per_1mtok: form.value.cache_read_price_per_1mtok,
        context_window: form.value.context_window,
      })
    } else {
      if (!form.value.provider_id) {
        dialogError.value = 'Please select a provider'
        saving.value = false
        return
      }
      await store.create({
        model_id: form.value.model_id,
        display_name: form.value.display_name || form.value.model_id,
        provider_id: form.value.provider_id,
        input_price_per_1mtok: form.value.input_price_per_1mtok,
        output_price_per_1mtok: form.value.output_price_per_1mtok,
        cache_write_5m_price_per_1mtok: form.value.cache_write_5m_price_per_1mtok,
        cache_write_1h_price_per_1mtok: form.value.cache_write_1h_price_per_1mtok,
        cache_read_price_per_1mtok: form.value.cache_read_price_per_1mtok,
        context_window: form.value.context_window,
      })
    }
    dialog.value = false
  } catch (e: any) {
    dialogError.value = e.response?.data?.error || 'Failed to save'
  } finally {
    saving.value = false
  }
}

async function handleDelete(id: number) {
  if (!confirm('Delete this model?')) return
  await store.remove(id)
}

onMounted(async () => {
  await providersStore.fetch()
  await store.fetch()
})
</script>
