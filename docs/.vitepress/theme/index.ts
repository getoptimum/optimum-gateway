// https://vitepress.dev/guide/custom-theme
import { h, nextTick, onMounted, watch } from "vue";
import type { Theme } from "vitepress";
import { useRoute } from "vitepress";
import DefaultTheme from 'vitepress/theme-without-fonts';
import mediumZoom from "medium-zoom";
import "./style.css";
import "./fonts.css";
import "virtual:group-icons.css";
import VersionSwitcher from "./VersionSwitcher.vue";

let zoom: ReturnType<typeof mediumZoom> | undefined;

function initZoom() {
  zoom?.detach();
  zoom = mediumZoom(".main img", { background: "var(--vp-c-bg)" });
}

export default {
  extends: DefaultTheme,
  Layout: () => {
    return h(DefaultTheme.Layout, null, {
      "nav-bar-content-before": () => h(VersionSwitcher),
    });
  },
  setup() {
    const route = useRoute();
    onMounted(initZoom);
    watch(
      () => route.path,
      () => nextTick(initZoom)
    );
  },
} satisfies Theme;
