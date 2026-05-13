<template>
    <div class="page">
        <div class="page-header">
            <div>
                <h1 class="page-title">Models</h1>
                <p class="page-sub">Registered inference models and pricing</p>
            </div>
            <button class="btn-primary" @click="openDialog()">
                <v-icon size="15">mdi-plus</v-icon>
                Add Model
            </button>
        </div>

        <div class="table-card">
            <v-data-table
                :headers="headers"
                :items="store.models"
                :loading="store.loading"
                density="comfortable"
                hide-default-footer
                :items-per-page="-1"
            >
                <template #item.full_id="{ item }">
                    <code class="mono-tag">{{ item.provider_key }}/{{ item.model_id }}</code>
                </template>
                <template #item.is_manual="{ item }">
                    <span class="status-chip" :class="item.is_manual ? 'status-chip--warning' : 'status-chip--on'">
                        {{ item.is_manual ? "Manual" : "Auto" }}
                    </span>
                </template>
                <template #item.pricing="{ item }">
                    <div class="pricing-cell">
                        <span class="pricing-pair">
                            <span class="pricing-key">in</span>
                            <span class="pricing-val">${{ item.input_price_per_1mtok }}</span>
                        </span>
                        <span class="pricing-sep">·</span>
                        <span class="pricing-pair">
                            <span class="pricing-key">out</span>
                            <span class="pricing-val">${{ item.output_price_per_1mtok }}</span>
                        </span>
                        <span v-if="item.cache_read_price_per_1mtok" class="pricing-sep">·</span>
                        <span v-if="item.cache_read_price_per_1mtok" class="pricing-pair">
                            <span class="pricing-key">cache</span>
                            <span class="pricing-val">${{ item.cache_read_price_per_1mtok }}</span>
                        </span>
                    </div>
                </template>
                <template #item.context_window="{ item }">
                    <span class="mono-val">{{ item.context_window ? (item.context_window / 1000).toFixed(0) + 'k' : '—' }}</span>
                </template>
                <template #item.actions="{ item }">
                    <div class="row-actions">
                        <button class="row-btn" title="Edit" @click="openEditDialog(item)">
                            <v-icon size="15">mdi-pencil-outline</v-icon>
                        </button>
                        <button class="row-btn row-btn--danger" title="Delete" @click="handleDelete(item.id)">
                            <v-icon size="15">mdi-delete-outline</v-icon>
                        </button>
                    </div>
                </template>
                <template #no-data>
                    <div class="empty-state">
                        <v-icon size="32" color="#4a4844">mdi-cube-off-outline</v-icon>
                        <p>No models registered. Add a provider first, then sync.</p>
                    </div>
                </template>
            </v-data-table>
        </div>

        <v-dialog v-model="dialog" max-width="500">
            <div class="dialog-card">
                <div class="dialog-header">
                    <h2 class="dialog-title">{{ editMode ? "Edit Model" : "Add Model" }}</h2>
                    <button class="dialog-close" @click="dialog = false">
                        <v-icon size="18">mdi-close</v-icon>
                    </button>
                </div>
                <div class="dialog-body">
                    <div v-if="!editMode" class="field-group">
                        <label class="field-label">Provider</label>
                        <select v-model="form.provider_id" class="field-select">
                            <option v-for="p in providerOptions" :key="p.value" :value="p.value">{{ p.text }}</option>
                        </select>
                    </div>
                    <div class="field-group">
                        <label class="field-label">Model ID</label>
                        <input v-model="form.model_id" class="field-input" placeholder="gpt-4o" />
                    </div>
                    <div class="field-group">
                        <label class="field-label">Display Name</label>
                        <input v-model="form.display_name" class="field-input" placeholder="GPT-4 Omni" />
                    </div>
                    <div class="price-grid">
                        <div class="field-group">
                            <label class="field-label">Input $/1M</label>
                            <input v-model.number="form.input_price_per_1mtok" type="number" step="0.01" class="field-input" />
                        </div>
                        <div class="field-group">
                            <label class="field-label">Output $/1M</label>
                            <input v-model.number="form.output_price_per_1mtok" type="number" step="0.01" class="field-input" />
                        </div>
                        <div class="field-group">
                            <label class="field-label">Cache Write 5m</label>
                            <input v-model.number="form.cache_write_5m_price_per_1mtok" type="number" step="0.01" class="field-input" />
                        </div>
                        <div class="field-group">
                            <label class="field-label">Cache Write 1h</label>
                            <input v-model.number="form.cache_write_1h_price_per_1mtok" type="number" step="0.01" class="field-input" />
                        </div>
                        <div class="field-group">
                            <label class="field-label">Cache Read</label>
                            <input v-model.number="form.cache_read_price_per_1mtok" type="number" step="0.01" class="field-input" />
                        </div>
                        <div class="field-group">
                            <label class="field-label">Context Window</label>
                            <input v-model.number="form.context_window" type="number" class="field-input" placeholder="128000" />
                        </div>
                    </div>
                    <div v-if="dialogError" class="alert alert--error">
                        <v-icon size="14">mdi-alert-circle-outline</v-icon>
                        {{ dialogError }}
                    </div>
                </div>
                <div class="dialog-footer">
                    <button class="btn-ghost" @click="dialog = false">Cancel</button>
                    <button class="btn-primary" @click="handleSave" :disabled="saving">
                        <span v-if="!saving">{{ editMode ? "Update" : "Create" }}</span>
                        <span v-else class="btn-spinner" />
                    </button>
                </div>
            </div>
        </v-dialog>
    </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import { useModelsStore } from "../stores/models";
import { useProvidersStore } from "../stores/providers";

const store = useModelsStore();
const providersStore = useProvidersStore();
const dialog = ref(false);
const editMode = ref(false);
const editingId = ref<number | null>(null);
const saving = ref(false);
const dialogError = ref("");

const form = ref({
    provider_id: null as number | null,
    model_id: "", display_name: "",
    input_price_per_1mtok: 0, output_price_per_1mtok: 0,
    cache_write_5m_price_per_1mtok: 0, cache_write_1h_price_per_1mtok: 0,
    cache_read_price_per_1mtok: 0, context_window: 0,
});

const providerOptions = computed(() =>
    providersStore.providers.map((p) => ({ text: p.name, value: p.id })),
);

const headers = [
    { title: "Model", key: "full_id", sortable: false },
    { title: "Provider", key: "provider_key" },
    { title: "Source", key: "is_manual" },
    { title: "Pricing ($/1M tok)", key: "pricing", sortable: false },
    { title: "Context", key: "context_window" },
    { title: "", key: "actions", sortable: false, width: 80 },
];

function openDialog() {
    editMode.value = false;
    editingId.value = null;
    dialogError.value = "";
    form.value = {
        provider_id: providersStore.providers[0]?.id || null,
        model_id: "", display_name: "",
        input_price_per_1mtok: 0, output_price_per_1mtok: 0,
        cache_write_5m_price_per_1mtok: 0, cache_write_1h_price_per_1mtok: 0,
        cache_read_price_per_1mtok: 0, context_window: 0,
    };
    dialog.value = true;
}

function openEditDialog(model: any) {
    editMode.value = true;
    editingId.value = model.id;
    dialogError.value = "";
    form.value = {
        provider_id: model.provider_id, model_id: model.model_id, display_name: model.display_name,
        input_price_per_1mtok: model.input_price_per_1mtok || 0,
        output_price_per_1mtok: model.output_price_per_1mtok || 0,
        cache_write_5m_price_per_1mtok: model.cache_write_5m_price_per_1mtok || 0,
        cache_write_1h_price_per_1mtok: model.cache_write_1h_price_per_1mtok || 0,
        cache_read_price_per_1mtok: model.cache_read_price_per_1mtok || 0,
        context_window: model.context_window || 0,
    };
    dialog.value = true;
}

async function handleSave() {
    saving.value = true;
    dialogError.value = "";
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
            });
        } else {
            if (!form.value.provider_id) { dialogError.value = "Please select a provider"; saving.value = false; return; }
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
            });
        }
        dialog.value = false;
    } catch (e: any) {
        dialogError.value = e.response?.data?.error || "Failed to save";
    } finally {
        saving.value = false;
    }
}

async function handleDelete(id: number) {
    if (!confirm("Delete this model?")) return;
    await store.remove(id);
}

onMounted(async () => {
    await providersStore.fetch();
    await store.fetch();
});
</script>

<style scoped>
@import '../styles/page-shared.css';

.mono-val {
    font-family: 'JetBrains Mono', monospace;
    font-size: 0.82rem;
    color: #7c7a75;
}
.pricing-cell {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
}
.pricing-pair {
    display: flex;
    align-items: center;
    gap: 3px;
}
.pricing-key {
    font-family: 'JetBrains Mono', monospace;
    font-size: 0.68rem;
    color: #4a4844;
    text-transform: uppercase;
    letter-spacing: 0.04em;
}
.pricing-val {
    font-family: 'JetBrains Mono', monospace;
    font-size: 0.78rem;
    color: #e8e6e1;
}
.pricing-sep {
    color: #2c2c32;
    font-size: 0.9rem;
}
.price-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
}
</style>
