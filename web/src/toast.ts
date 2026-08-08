// Minimal toast bus. Anywhere in the app can surface a transient message via
// toast('…'); <Toaster/> in App renders them. Used so silent save failures
// become visible instead of the UI wrongly implying "saved".
export function toast(message: string) {
  window.dispatchEvent(new CustomEvent('salt:toast', { detail: message }));
}
