<template>
  <div class="page">
    <PageHeader :title="$t('models.title')" :subtitle="$t('models.subtitle')">
      <button v-if="isAdmin" class="btn-primary" @click="openDialog()">
        <v-icon size="15">mdi-plus</v-icon>
        {{ $t("models.addModel") }}
      </button>
    </PageHeader>

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
          <MonoTag>{{ item.provider_key }}/{{ item.model_id }}</MonoTag>
        </template>
        <template #item.is_manual="{ item }">
          <StatusChip :variant="item.is_manual ? 'warning' : 'on'">
            {{ item.is_manual ? $t("models.manual") : $t("models.auto") }}
          </StatusChip>
        </template>
        <template #item.pricing="{ item }">
          <div class="pricing-cell">
            <span class="pricing-pair">
              <span class="pricing-key">{{ $t("models.pricingIn") }}</span>
              <span class="pricing-val">${{ item.input_price_per_1mtok }}</span>
            </span>
            <span class="pricing-sep">·</span>
            <span class="pricing-pair">
              <span class="pricing-key">{{ $t("models.pricingOut") }}</span>
              <span class="pricing-val"
                >${{ item.output_price_per_1mtok }}</span
              >
            </span>
            <span v-if="item.cache_read_price_per_1mtok" class="pricing-sep"
              >·</span
            >
            <span v-if="item.cache_read_price_per_1mtok" class="pricing-pair">
              <span class="pricing-key">{{ $t("models.pricingCache") }}</span>
              <span class="pricing-val"
                >${{ item.cache_read_price_per_1mtok }}</span
              >
            </span>
          </div>
        </template>
        <template #item.context_window="{ item }">
          <span class="mono-val">{{
            item.context_window
              ? (item.context_window / 1000).toFixed(0) + "k"
              : "—"
          }}</span>
        </template>
        <template #item.actions="{ item }">
          <div class="row-actions">
            <button class="row-btn" title="Edit" @click="openEditDialog(item)">
              <v-icon size="15">mdi-pencil-outline</v-icon>
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
          <EmptyState icon="mdi-cube-off-outline" :text="$t('models.noModels')" />
        </template>
      </v-data-table>
    </div>

    <!-- Mobile cards -->
    <div class="mobile-cards">
      <MobileDataCard
        v-for="m in store.models"
        :key="m.id"
        :items="[
          {
            label: $t('models.model'),
            value: m.provider_key + '/' + m.model_id,
          },
          { label: $t('models.provider'), value: m.provider_key },
          {
            label: $t('models.source'),
            value: m.is_manual ? $t('models.manual') : $t('models.auto'),
          },
          {
            label: $t('models.pricing'),
            value:
              '$' +
              (m.input_price_per_1mtok ?? 0) +
              ' / $' +
              (m.output_price_per_1mtok ?? 0),
          },
          {
            label: $t('models.context'),
            value: m.context_window
              ? (m.context_window / 1000).toFixed(0) + 'k'
              : '—',
          },
        ]"
      >
        <template v-if="isAdmin" #actions>
          <button class="row-btn" title="Edit" @click="openEditDialog(m)">
            <v-icon size="15">mdi-pencil-outline</v-icon>
          </button>
          <button
            class="row-btn row-btn--danger"
            title="Delete"
            @click="handleDelete(m.id)"
          >
            <v-icon size="15">mdi-delete-outline</v-icon>
          </button>
        </template>
      </MobileDataCard>
      <EmptyState
        v-if="!store.models.length"
        icon="mdi-cube-off-outline"
        :text="$t('models.noModels')"
      />
    </div>

    <v-dialog
      v-model="dialog"
      :max-width="isMobile ? undefined : 500"
      :fullscreen="isMobile"
    >
      <div class="dialog-card">
        <div class="dialog-header">
          <h2 class="dialog-title">
            {{ editMode ? $t("models.editModel") : $t("models.addModel") }}
          </h2>
          <button class="dialog-close" @click="dialog = false">
            <v-icon size="18">mdi-close</v-icon>
          </button>
        </div>
        <div class="dialog-body">
          <div v-if="!editMode" class="field-group">
            <label class="field-label">{{ $t("models.provider") }}</label>
            <select v-model="form.provider_id" class="field-select">
              <option
                v-for="p in providerOptions"
                :key="p.value"
                :value="p.value"
              >
                {{ p.text }}
              </option>
            </select>
          </div>
          <div class="field-group">
            <label class="field-label">{{ $t("models.modelId") }}</label>
            <input
              v-model="form.model_id"
              class="field-input"
              placeholder="gpt-4o"
            />
          </div>
          <div class="field-group">
            <label class="field-label">{{ $t("models.displayName") }}</label>
            <input
              v-model="form.display_name"
              class="field-input"
              placeholder="GPT-4 Omni"
            />
          </div>
          <div class="price-grid">
            <div class="field-group">
              <label class="field-label">{{ $t("models.inputPrice") }}</label>
              <input
                v-model.number="form.input_price_per_1mtok"
                type="number"
                step="0.01"
                class="field-input"
              />
            </div>
            <div class="field-group">
              <label class="field-label">{{ $t("models.outputPrice") }}</label>
              <input
                v-model.number="form.output_price_per_1mtok"
                type="number"
                step="0.01"
                class="field-input"
              />
            </div>
            <div class="field-group">
              <label class="field-label">{{ $t("models.cacheWrite5m") }}</label>
              <input
                v-model.number="form.cache_write_5m_price_per_1mtok"
                type="number"
                step="0.01"
                class="field-input"
              />
            </div>
            <div class="field-group">
              <label class="field-label">{{ $t("models.cacheWrite1h") }}</label>
              <input
                v-model.number="form.cache_write_1h_price_per_1mtok"
                type="number"
                step="0.01"
                class="field-input"
              />
            </div>
            <div class="field-group">
              <label class="field-label">{{ $t("models.cacheRead") }}</label>
              <input
                v-model.number="form.cache_read_price_per_1mtok"
                type="number"
                step="0.01"
                class="field-input"
              />
            </div>
            <div class="field-group">
              <label class="field-label">{{
                $t("models.contextWindow")
              }}</label>
              <input
                v-model.number="form.context_window"
                type="number"
                class="field-input"
                placeholder="128000"
              />
            </div>
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
              editMode ? $t("common.update") : $t("common.create")
            }}</span>
            <span v-else class="btn-spinner" />
          </button>
        </div>
      </div>
    </v-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import { useI18n } from "vue-i18n";
import { useModelsStore } from "../stores/models";
import { useProvidersStore } from "../stores/providers";
import { useAuthStore } from "../stores/auth";
import MobileDataCard from "../components/MobileDataCard.vue";
import PageHeader from "../components/PageHeader.vue";
import EmptyState from "../components/EmptyState.vue";
import StatusChip from "../components/StatusChip.vue";
import MonoTag from "../components/MonoTag.vue";
import { useMobile } from "../composables/useMobile";

const { t } = useI18n();
const store = useModelsStore();
const providersStore = useProvidersStore();
const auth = useAuthStore();
const isAdmin = computed(() => !!auth.user?.is_admin);
const dialog = ref(false);
const editMode = ref(false);
const editingId = ref<number | null>(null);
const saving = ref(false);
const dialogError = ref("");

const { isMobile } = useMobile();

const form = ref({
  provider_id: null as number | null,
  model_id: "",
  display_name: "",
  input_price_per_1mtok: 0,
  output_price_per_1mtok: 0,
  cache_write_5m_price_per_1mtok: 0,
  cache_write_1h_price_per_1mtok: 0,
  cache_read_price_per_1mtok: 0,
  context_window: 0,
});

const providerOptions = computed(() =>
  providersStore.providers.map((p) => ({ text: p.name, value: p.id })),
);

const headers = computed(() => {
  const cols: Record<string, unknown>[] = [
    { title: t("models.model"), key: "full_id", sortable: false },
    { title: t("models.provider"), key: "provider_key" },
    { title: t("models.source"), key: "is_manual" },
    { title: t("models.pricing"), key: "pricing", sortable: false },
    { title: t("models.context"), key: "context_window" },
  ];
  if (isAdmin.value) {
    cols.push({ title: "", key: "actions", sortable: false, width: 80 });
  }
  return cols;
});

function openDialog() {
  editMode.value = false;
  editingId.value = null;
  dialogError.value = "";
  form.value = {
    provider_id: providersStore.providers[0]?.id || null,
    model_id: "",
    display_name: "",
    input_price_per_1mtok: 0,
    output_price_per_1mtok: 0,
    cache_write_5m_price_per_1mtok: 0,
    cache_write_1h_price_per_1mtok: 0,
    cache_read_price_per_1mtok: 0,
    context_window: 0,
  };
  dialog.value = true;
}

function openEditDialog(model: any) {
  editMode.value = true;
  editingId.value = model.id;
  dialogError.value = "";
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
        cache_write_5m_price_per_1mtok:
          form.value.cache_write_5m_price_per_1mtok,
        cache_write_1h_price_per_1mtok:
          form.value.cache_write_1h_price_per_1mtok,
        cache_read_price_per_1mtok: form.value.cache_read_price_per_1mtok,
        context_window: form.value.context_window,
      });
    } else {
      if (!form.value.provider_id) {
        dialogError.value = t("models.selectProvider");
        saving.value = false;
        return;
      }
      await store.create({
        model_id: form.value.model_id,
        display_name: form.value.display_name || form.value.model_id,
        provider_id: form.value.provider_id,
        input_price_per_1mtok: form.value.input_price_per_1mtok,
        output_price_per_1mtok: form.value.output_price_per_1mtok,
        cache_write_5m_price_per_1mtok:
          form.value.cache_write_5m_price_per_1mtok,
        cache_write_1h_price_per_1mtok:
          form.value.cache_write_1h_price_per_1mtok,
        cache_read_price_per_1mtok: form.value.cache_read_price_per_1mtok,
        context_window: form.value.context_window,
      });
    }
    dialog.value = false;
  } catch (e: any) {
    dialogError.value = e.response?.data?.error || t("models.saveFailed");
  } finally {
    saving.value = false;
  }
}

async function handleDelete(id: number) {
  if (!confirm(t("models.deleteConfirm"))) return;
  await store.remove(id);
}

onMounted(async () => {
  await providersStore.fetch();
  await store.fetch();
});
</script>

<style scoped>
@import "../styles/page-shared.css";

.mono-val {
  font-family: "JetBrains Mono", monospace;
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
  font-family: "JetBrains Mono", monospace;
  font-size: 0.68rem;
  color: #4a4844;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.pricing-val {
  font-family: "JetBrains Mono", monospace;
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
@media (max-width: 768px) {
  .price-grid {
    grid-template-columns: 1fr;
  }
}
</style>
