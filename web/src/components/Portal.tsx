import type { ReactNode } from 'react';
import { createPortal } from 'react-dom';

// Renders children into document.body. Modals MUST portal out: the mobile
// sidebar uses a CSS transform for its slide animation, and a transformed
// ancestor becomes the containing block for position:fixed descendants — so a
// modal rendered inside the sidebar would be trapped in the sidebar's box
// instead of covering the viewport. Portaling to <body> escapes that.
export default function Portal({ children }: { children: ReactNode }) {
  return createPortal(children, document.body);
}
