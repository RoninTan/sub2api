/**
 * User Subscription API
 * API for regular users to view their own subscriptions and progress
 */

import { apiClient } from './client'
import type { UserSubscription, SubscriptionProgress } from '@/types'

/**
 * Subscription summary for user dashboard
 */
export interface SubscriptionSummary {
  active_count: number
  subscriptions: Array<{
    id: number
    group_name: string
    status: string
    daily_progress: number | null
    weekly_progress: number | null
    monthly_progress: number | null
    expires_at: string | null
    days_remaining: number | null
  }>
}

/**
 * Get list of current user's subscriptions
 */
export async function getMySubscriptions(): Promise<UserSubscription[]> {
  const response = await apiClient.get<UserSubscription[]>('/subscriptions')
  return response.data
}

/**
 * Get current user's active subscriptions
 */
export async function getActiveSubscriptions(): Promise<UserSubscription[]> {
  const response = await apiClient.get<UserSubscription[]>('/subscriptions/active')
  return response.data
}

/**
 * Get progress for all user's active subscriptions
 */
export async function getSubscriptionsProgress(): Promise<SubscriptionProgress[]> {
  const response = await apiClient.get<SubscriptionProgress[]>('/subscriptions/progress')
  return response.data
}

/**
 * Get subscription summary for dashboard display
 */
export async function getSubscriptionSummary(): Promise<SubscriptionSummary> {
  const response = await apiClient.get<SubscriptionSummary>('/subscriptions/summary')
  return response.data
}

/**
 * Get progress for a specific subscription
 */
export async function getSubscriptionProgress(
  subscriptionId: number
): Promise<SubscriptionProgress> {
  const response = await apiClient.get<SubscriptionProgress>(
    `/subscriptions/${subscriptionId}/progress`
  )
  return response.data
}

/**
 * Self-reset allowance for one subscription.
 */
export interface SelfResetStatus {
  subscription_id: number
  group_id: number
  group_name: string
  enabled: boolean
  max_resets: number
  used: number
  remaining: number
  period: string
  period_key: string
  period_resets_at: string
  scope: string
  can_reset: boolean
  reason: string
  daily_usage_usd: number
  daily_limit_usd?: number
}

export interface SelfResetResult {
  subscription_id: number
  used: number
  remaining: number
  max_resets: number
  period: string
  period_key: string
  scope: string
}

/**
 * Get current user's self-reset allowance per active subscription
 */
export async function getSelfResetStatus(): Promise<SelfResetStatus[]> {
  const response = await apiClient.get<SelfResetStatus[]>('/subscriptions/self-reset/status')
  return response.data
}

/**
 * Consume one self-reset chance for a subscription
 */
export async function selfResetQuota(subscriptionId: number): Promise<SelfResetResult> {
  const response = await apiClient.post<SelfResetResult>(
    `/subscriptions/self-reset/${subscriptionId}/quota`
  )
  return response.data
}

export default {
  getMySubscriptions,
  getActiveSubscriptions,
  getSubscriptionsProgress,
  getSubscriptionSummary,
  getSubscriptionProgress,
  getSelfResetStatus,
  selfResetQuota
}
