<script setup lang="ts">
import { computed } from 'vue'
import { useData, useRoute } from 'vitepress'
import VPNavBarMenuGroup from 'vitepress/dist/client/theme-default/components/VPNavBarMenuGroup.vue'

const route = useRoute()
const { theme } = useData()

const parsed = computed(() => {
  const m = route.path.match(/\/versions\/([^/]+)(?:\/(.*))?$/)
  return { ver: m?.[1] ?? '', rest: m?.[2] ?? '' }
})

const current = computed(() => parsed.value.ver)

const label = computed(() => {
  const v = current.value
  if (!v) return 'Versions'
  return v === 'latest' ? `Latest (${theme.value.latest})` : v
})

function pageLink(ver: string): string {
  const rest = parsed.value.rest
  const fallback = `/versions/${ver}/`
  if (!rest) return fallback
  const target = `/versions/${ver}/${rest}`
  const group = theme.value.sidebar[`/versions/${ver}/`]
  const items = Array.isArray(group) ? group[0]?.items ?? [] : []
  return items.some((i: { link?: string }) => i.link === target) ? target : fallback
}

const items = computed(() => [
  { text: `Latest (${theme.value.latest})`, link: pageLink('latest') },
  ...theme.value.versions.slice().reverse().map((v: string) => ({
    text: v,
    link: pageLink(v)
  }))
])
</script>

<template>
  <div class="version-switcher">
    <ClientOnly>
      <VPNavBarMenuGroup :item="{ text: label, items }" />
    </ClientOnly>
  </div>
</template>
