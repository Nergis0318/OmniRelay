import { ref } from "vue";

export function useCopyToClipboard() {
  const copied = ref(false);
  let timer: ReturnType<typeof setTimeout> | undefined;

  function copy(text: string) {
    navigator.clipboard.writeText(text);
    copied.value = true;
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => {
      copied.value = false;
    }, 2000);
  }

  return { copied, copy };
}
