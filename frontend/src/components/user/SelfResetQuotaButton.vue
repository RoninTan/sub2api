<template>
  <div v-if="status && status.enabled" class="rounded-xl border border-gray-200 p-3 dark:border-dark-600">
    <div class="flex items-center justify-between gap-3">
      <div class="min-w-0">
        <p class="text-sm font-medium text-gray-800 dark:text-gray-200">
          {{ t('userSubscriptions.selfReset.title') }}
        </p>
        <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
          {{
            t('userSubscriptions.selfReset.remaining', {
              remaining: status.remaining,
              max: status.max_resets
            })
          }}
        </p>
      </div>
      <button
        type="button"
        class="shrink-0 rounded-lg px-3 py-1.5 text-xs font-semibold text-white transition-colors disabled:cursor-not-allowed disabled:opacity-50"
        :class="status.can_reset ? 'bg-primary-600 hover:bg-primary-700' : 'bg-gray-400'"
        :disabled="!status.can_reset || busy"
        @click="handleReset"
      >
        {{ busy ? t('userSubscriptions.selfReset.resetting') : t('userSubscriptions.selfReset.action') }}
      </button>
    </div>
    <p v-if="!status.can_reset && status.reason !== 'ok'" class="mt-2 text-xs text-gray-400">
      {{ reasonText }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import subscriptionsAPI, { type SelfResetStatus } from '@/api/subscriptions'
import { useAppStore } from '@/stores/app'

const props = defineProps<{
  subscriptionId: number
  dailyUsage?: number
  dailyLimit?: number
}>()

const emit = defineEmits<{ (e: 'reset'): void }>()
const { t } = useI18n()
const appStore = useAppStore()
const status = ref<SelfResetStatus | null>(null)
const busy = ref(false)

const reasonText = computed(() => {
  if (!status.value) return ''
  switch (status.value.reason) {
    case 'disabled':
      return t('userSubscriptions.selfReset.reason.disabled')
    case 'inactive':
      return t('userSubscriptions.selfReset.reason.inactive')
    case 'not_subscription':
      return t('userSubscriptions.selfReset.reason.notSubscription')
    case 'no_daily_limit':
      return t('userSubscriptions.selfReset.reason.noDailyLimit')
    case 'no_resets':
      return t('userSubscriptions.selfReset.reason.noResets')
    default:
      return ''
  }
})

async function loadStatus() {
  try {
    const list = await subscriptionsAPI.getSelfResetStatus()
    status.value = list.find((item) => item.subscription_id === props.subscriptionId) || null
  } catch (error) {
    console.error('Failed to load self-reset status:', error)
  }
}

async function handleReset() {
  if (!status.value || !status.value.can_reset || busy.value) return
  const usage = props.dailyUsage ?? status.value.daily_usage_usd ?? 0
  const limit = props.dailyLimit ?? status.value.daily_limit_usd ?? 0
  const message =
    usage <= 0
      ? t('userSubscriptions.selfReset.zeroUsageConfirm', {
          remaining: status.value.remaining,
          max: status.value.max_resets
        })
      : t('userSubscriptions.selfReset.confirm', {
          used: usage.toFixed(2),
          limit: limit.toFixed(2),
          remaining: status.value.remaining,
          max: status.value.max_resets
        })
  if (!window.confirm(message)) return

  busy.value = true
  try {
    const result = await subscriptionsAPI.selfResetQuota(props.subscriptionId)
    appStore.showSuccess(
      t('userSubscriptions.selfReset.success', {
        remaining: result.remaining,
        max: result.max_resets
      })
    )
    await loadStatus()
    emit('reset')
  } catch (error: any) {
    const message = error?.response?.data?.message || t('userSubscriptions.selfReset.failed')
    appStore.showError(message)
  } finally {
    busy.value = false
  }
}

onMounted(loadStatus)
</script>
