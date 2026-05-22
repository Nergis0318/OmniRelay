<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h1 class="page-title">{{ $t("providers.title") }}</h1>
        <p class="page-sub">{{ $t("providers.subtitle") }}</p>
      </div>
      <button class="btn-primary" @click="openDialog()">
        <v-icon size="15">mdi-plus</v-icon>
        {{ $t("providers.addProvider") }}
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
          <span
            class="status-chip"
            :class="item.is_active ? 'status-chip--on' : 'status-chip--off'"
          >
            {{
              item.is_active ? $t("providers.active") : $t("providers.inactive")
            }}
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
            <button
              class="row-btn"
              title="Sync Models"
              @click="handleSync(item.id)"
            >
              <v-icon size="15">mdi-sync</v-icon>
            </button>
            <button
              class="row-btn row-btn--danger"
              title="Delete"
              @click="handleDelete(item.id)"
            >
              <v-icon size="15">mdi-delete-outline</v-icon>
            </button>
          </div>
        </template>
        <template #no-data>
          <div class="empty-state">
            <v-icon size="32" color="#4a4844">mdi-server-off</v-icon>
            <p>{{ $t("providers.noProviders") }}</p>
          </div>
        </template>
      </v-data-table>
    </div>

    <!-- Mobile cards -->
    <div class="mobile-cards">
      <MobileDataCard
        v-for="p in store.providers"
        :key="p.id"
        :items="[
          { label: $t('providers.key'), value: p.provider_key },
          { label: $t('providers.name'), value: p.name },
          { label: $t('providers.type'), value: p.provider_type },
          { label: $t('providers.status'), value: p.is_active ? $t('providers.active') : $t('providers.inactive') },
        ]"
      >
        <template #actions>
          <button class="row-btn" title="Edit" @click="openDialog(p)">
            <v-icon size="15">mdi-pencil-outline</v-icon>
          </button>
          <button class="row-btn" title="Sync Models" @click="handleSync(p.id)">
            <v-icon size="15">mdi-sync</v-icon>
          </button>
          <button class="row-btn row-btn--danger" title="Delete" @click="handleDelete(p.id)">
            <v-icon size="15">mdi-delete-outline</v-icon>
          </button>
        </template>
      </MobileDataCard>
      <div v-if="!store.providers.length" class="empty-state">
        <v-icon size="32" color="#4a4844">mdi-server-off</v-icon>
        <p>{{ $t("providers.noProviders") }}</p>
      </div>
    </div>

    <!-- Dialog -->
    <v-dialog v-model="dialog" :max-width="isMobile ? undefined : 520" :fullscreen="isMobile">
      <div class="dialog-card">
        <div class="dialog-header">
          <h2 class="dialog-title">
            {{
              editing
                ? $t("providers.editProvider")
                : $t("providers.addProvider")
            }}
          </h2>
          <button class="dialog-close" @click="dialog = false">
            <v-icon size="18">mdi-close</v-icon>
          </button>
        </div>

        <div class="dialog-body">
          <div class="field-group">
            <label class="field-label">{{ $t("providers.providerKey") }}</label>
            <input
              v-model="form.provider_key"
              class="field-input"
              placeholder="e.g. openai, my-llama"
              :disabled="!!editing"
            />
          </div>
          <div class="field-group">
            <label class="field-label">{{ $t("providers.displayName") }}</label>
            <input
              v-model="form.name"
              class="field-input"
              placeholder="e.g. OpenAI"
            />
          </div>
          <div class="field-group">
            <label class="field-label">{{ $t("providers.apiBaseUrl") }}</label>
            <input
              v-model="form.api_base_url"
              class="field-input"
              placeholder="https://api.openai.com/v1"
            />
          </div>
          <div class="field-group">
            <label class="field-label">{{ $t("providers.apiKey") }}</label>
            <input
              v-model="form.api_key"
              type="password"
              class="field-input"
              :placeholder="editing ? $t('providers.leaveEmpty') : 'sk-...'"
            />
          </div>
          <div class="field-group">
            <label class="field-label">{{
              $t("providers.providerType")
            }}</label>
            <select v-model="form.provider_type" class="field-select">
              <option v-for="t in providerTypes" :key="t" :value="t">
                {{ t }}
              </option>
            </select>
          </div>
          <label class="checkbox-row">
            <input type="checkbox" v-model="form.auto_sync" class="checkbox" />
            <div>
              <span class="checkbox-label">{{ $t("providers.autoSync") }}</span>
              <span class="checkbox-hint">{{
                $t("providers.autoSyncHint")
              }}</span>
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
          <button class="btn-ghost" @click="dialog = false">
            {{ $t("common.cancel") }}
          </button>
          <button class="btn-primary" @click="handleSave" :disabled="saving">
            <span v-if="!saving">{{
              editing ? $t("common.update") : $t("common.create")
            }}</span>
            <span v-else class="btn-spinner" />
          </button>
        </div>
      </div>
    </v-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from "vue";
import { useI18n } from "vue-i18n";
import { useProvidersStore } from "../stores/providers";
import MobileDataCard from "../components/MobileDataCard.vue";

const { t } = useI18n();
const store = useProvidersStore();
const isMobile = ref(false);
function checkMobile() {
  isMobile.value = window.innerWidth <= 768;
}
onMounted(() => {
  checkMobile();
  window.addEventListener("resize", checkMobile);
  store.fetch();
});
onUnmounted(() => {
  window.removeEventListener("resize", checkMobile);
});

const dialog = ref(false);
const editing = ref<any>(null);
const saving = ref(false);
const dialogError = ref("");
const syncResult = ref("");
const providerTypes = ["openai", "anthropic", "lmstudio", "ollama", "gemini"];

const form = ref({
  provider_key: "",
  name: "",
  api_base_url: "",
  api_key: "",
  provider_type: "openai",
  auto_sync: true,
});

const headers = computed(() => [
  { title: t("providers.key"), key: "provider_key", sortable: true },
  { title: t("providers.name"), key: "name", sortable: true },
  { title: t("providers.type"), key: "provider_type" },
  { title: t("providers.status"), key: "is_active" },
  {
    title: t("providers.actions"),
    key: "actions",
    sortable: false,
    align: "end" as const,
  },
]);

function openDialog(provider?: any) {
  dialogError.value = "";
  syncResult.value = "";
  if (provider) {
    editing.value = provider;
    form.value = { ...provider, api_key: "", auto_sync: false };
  } else {
    editing.value = null;
    form.value = {
      provider_key: "",
      name: "",
      api_base_url: "",
      api_key: "",
      provider_type: "openai",
      auto_sync: true,
    };
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
        syncResult.value = t("providers.syncedModels", {
          count: data.model_count,
        });
      }
    } else {
      const created = await store.create(form.value);
      if (form.value.auto_sync) {
        const { data } = await store.syncModels(created.id);
        syncResult.value = t("providers.syncedModels", {
          count: data.model_count,
        });
      }
    }
    dialog.value = false;
  } catch (e: any) {
    dialogError.value = e.response?.data?.error || t("providers.saveFailed");
  } finally {
    saving.value = false;
  }
}

async function handleSync(id: number) {
  syncResult.value = "";
  try {
    const { data } = await store.syncModels(id);
    syncResult.value = t("providers.syncedModels", { count: data.model_count });
  } catch (e: any) {
    dialogError.value = e.response?.data?.error || t("providers.syncFailed");
  }
}

async function handleDelete(id: number) {
  if (!confirm(t("providers.deleteConfirm"))) return;
  await store.remove(id);
}

</script>

<style scoped>
@import "../styles/page-shared.css";
.mobile-cards {
  display: none;
}
@media (max-width: 768px) {
  .v-data-table {
    display: none;
  }
  .mobile-cards {
    display: block;
  }
}
</style>
