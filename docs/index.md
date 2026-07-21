<script setup>
import { useRouter, withBase } from 'vitepress'

const router = useRouter()
const target = withBase('/versions/latest/')

if (typeof window !== 'undefined') {
  router.go(target)
}
</script>

Redirecting to <a :href="target">latest</a>...
