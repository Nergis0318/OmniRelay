<template>
    <div class="page">
        <div class="page-header">
            <div>
                <h1 class="page-title">Providers</h1>
                <p class="page-sub">Manage upstream LLM provider connections</p>
            </div>
            <button class="btn-primary" @click="openDialog()">
                <v-icon size="15">mdi-plus</v-icon>
                Add Provider
            </button>
        </div>

        <div class="table-card">
            <v-data-table
                :headers="headers"
                :items="store.providers"
                :loading="store.loading"
                density="comfortable"
                hide-default-footer
                :items-per-page="-1"
            >
                <template #item.provider_key="{ item }">
                    <code class="mono-tag">{{ item.provider_key }}</code>
                </template>
                <template #item.is_active="{ item }">
                    <span class="status-chip" :class="item.is_active ? 'status-chip--on' : 'status-chip--off'">
                        {{ item.is_active ? "Active" : "Inactive" }}
                    </span>
                </template>
                <template #item.provider_type="{ item }">
                    <span class="type-chip">{{ item.provider_type }}</span>
                </template>
                <template #item.actions="{ item }">
                    <div class="row-actions">
                        <button class="row-btn" title="Edit" @click="openDialog(item)">
                            <v-icon size="15">mdi-pencil-outline</v-icon>
                        </button>
                        <button class="row-btn" title="Sync Models" @click="handleSync(item.id)">
                            <v-icon size="15">mdi-sync</v-icon>
                        </button>
                        <button class="row-btn row-btn--danger" title="Delete" @click="handleDelete(item.id)">
                            <v-icon size="15">mdi-delete-outline</v-icon>
                        </button>
                    </div>
                </template>
                <template #no-data>
                    <div class="empty-state">
                        <v-icon size="32" color="#4a4844">mdi-server-off</v-icon>
                        <p>No providers yet. Add one to get started.</p>
                    </div>
                </template>
            </v-data-table>
        </div>

        <!-- Dialog -->
        <v-dialog v-model="dialog" max-width="520">
            <div class="dialog-card">
                <div class="dialog-header">
                    <h2 class="dialog-title">{{ editing ? "Edit Provider" : "Add Provider" }}</h2>
                    <button class="dialog-close" @click="dialog = false">
                        <v-icon size="18">mdi-close</v-icon>
                    </button>
                </div>

                <div class="dialog-body">
                    <div class="field-group">
                        <label class="field-label">Provider Key</label>
                        <input v-model="form.provider_key" class="field-input" placeholder="e.g. openai, my-llama" :disabled="!!editing" />
                    </div>
                    <div class="field-group">
                        <label class="field-label">Display Name</label>
                        <input v-model="form.name" class="field-input" placeholder="e.g. OpenAI" />
                    </div>
                    <div class="field-group">
                        <label class="field-label">API Base URL</label>
                        <input v-model="form.api_base_url" class="field-input" placeholder="https://api.openai.com/v1" />
                    </div>
                    <div class="field-group">
                        <label class="field-label">API Key</label>
                        <input v-model="form.api_key" type="password" class="field-input" :placeholder="editing ? '(leave empty to keep)' : 'sk-...'" />
                    </div>
                    <div class="field-group">
                        <label class="field-label">Provider Type</label>
                        <select v-model="form.provider_type" class="field-select">
                            <option v-for="t in providerTypes" :key="t" :value="t">{{ t }}</option>
                        </select>
                    </div>
                    <label class="checkbox-row">
                        <input type="checkbox" v-model="form.auto_sync" class="checkbox" />
                        <div>
                            <span class="checkbox-label">Auto-sync models after save</span>
                            <span class="checkbox-hint">Fetch available models from provider API</span>
                        </div>
                    </label>

                    <div v-if="syncResult" class="alert alert--success">
                        <v-icon size="14">mdi-check-circle-outline</v-icon>
                        {{ syncResult }}
                    </div>
                    <div v-if="dialogError" class="alert alert--error">
                        <v-icon size="14">mdi-alert-circle-outline</v-icon>
                        {{ dialogError }}
                    </div>
                </div>

                <div class="dialog-footer">
                    <button class="btn-ghost" @click="dialog = false">Cancel</button>
                    <button class="btn-primary" @click="handleSave" :disabled="saving">
                        <span v-if="!saving">{{ editing ? "Update" : "Create" }}</span>
                        <span v-else class="btn-spinner" />
                    </button>
                </div>
            </div>
        </v-dialog>
    </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from "vue";
import { useProvidersStore } from "../stores/providers";

const store = useProvidersStore();
const dialog = ref(false);
const editing = ref<any>(null);
const saving = ref(false);
const dialogError = ref("");
const syncResult = ref("");
const providerTypes = ["openai", "anthropic", "lmstudio", "ollama", "gemini"];

const form = ref({
    provider_key: "", name: "", api_base_url: "",
    api_key: "", provider_type: "openai", auto_sync: true,
});

const headers = [
    { title: "Key", key: "provider_key", sortable: true },
    { title: "Name", key: "name", sortable: true },
    { title: "Type", key: "provider_type" },
    { title: "Status", key: "is_active" },
    { title: "Actions", key: "actions", sortable: false, align: "end" as const },
];

function openDialog(provider?: any) {
    dialogError.value = "";
    syncResult.value = "";
    if (provider) {
        editing.value = provider;
        form.value = { ...provider, api_key: "", auto_sync: false };
    } else {
        editing.value = null;
        form.value = { provider_key: "", name: "", api_base_url: "", api_key: "", provider_type: "openai", auto_sync: true };
    }
    dialog.value = true;
}

async function handleSave() {
    saving.value = true;
    dialogError.value = "";
    syncResult.value = "";
    try {
        if (editing.value) {
            await store.update(editing.value.id, form.value);
            if (form.value.auto_sync) {
                const { data } = await store.syncModels(editing.value.id);
                syncResult.value = `Synced ${data.model_count} models`;
            }
        } else {
            const created = await store.create(form.value);
            if (form.value.auto_sync) {
                const { data } = await store.syncModels(created.id);
                syncResult.value = `Synced ${data.model_count} models`;
            }
        }
        dialog.value = false;
    } catch (e: any) {
        dialogError.value = e.response?.data?.error || "Failed to save";
    } finally {
        saving.value = false;
    }
}

async function handleSync(id: number) {
    syncResult.value = "";
    try {
        const { data } = await store.syncModels(id);
        syncResult.value = `Synced ${data.model_count} models`;
    } catch (e: any) {
        dialogError.value = e.response?.data?.error || "Sync failed";
    }
}

async function handleDelete(id: number) {
    if (!confirm("Delete this provider and all its models?")) return;
    await store.remove(id);
}

onMounted(() => store.fetch());
</script>

<style scoped>
@import '../styles/page-shared.css';
</style>
