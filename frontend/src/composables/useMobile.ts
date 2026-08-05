import { ref, onMounted, onUnmounted } from "vue";

const MOBILE_BREAKPOINT = 768;

export function useMobile() {
  const isMobile = ref(false);
  let mql: MediaQueryList | null = null;

  function handleChange(e: MediaQueryListEvent) {
    isMobile.value = e.matches;
  }

  onMounted(() => {
    mql = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT}px)`);
    isMobile.value = mql.matches;
    mql.addEventListener("change", handleChange);
  });

  onUnmounted(() => {
    mql?.removeEventListener("change", handleChange);
  });

  return { isMobile };
}