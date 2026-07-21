<script setup lang="ts">
import { computed } from 'vue'
import { useData, useRoute } from 'vitepress'
import VPNavBarMenuGroup from 'vitepress/dist/client/theme-default/components/VPNavBarMenuGroup.vue'

const route = useRoute()
const { theme } = useData()

const current = computed(() => {
  const m = route.path.match(/\/versions\/([^/]+)\//)
  return m?.[1] ?? ''
})

const label = computed(() => {
  const v = current.value
  if (!v) return 'Versions'
  return v === 'latest' ? `Latest (${theme.value.latest})` : v
})

const items = computed(() => [
  { text: `Latest (${theme.value.latest})`, link: '/versions/latest/' },
  ...theme.value.versions.slice().reverse().map((v: string) => ({
    text: v,
    link: `/versions/${v}/`
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
