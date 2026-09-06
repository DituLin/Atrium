/**
 * Off-screen image decode used for slideshow preloading (design §7.3: the next
 * image is fetched one interval ahead, and at most two decoded images are kept
 * alive at any moment).
 *
 * The returned handle owns the only reference to the decoded bitmap; dropping
 * it lets the engine reclaim the memory, which matters on a TV with a few
 * hundred megabytes of headroom.
 */

export interface DecodedImage {
  id: string;
  src: string;
  element: HTMLImageElement;
}

export interface DecodeResult {
  ok: boolean;
  image: DecodedImage | null;
}

export function decodeImage(id: string, src: string): Promise<DecodeResult> {
  return new Promise((resolve) => {
    if (typeof Image !== 'function') {
      resolve({ ok: false, image: null });
      return;
    }
    const element = new Image();
    const settle = (ok: boolean): void => {
      element.onload = null;
      element.onerror = null;
      resolve({ ok, image: ok ? { id, src, element } : null });
    };
    element.onload = () => settle(true);
    element.onerror = () => settle(false);
    // `decoding = async` keeps the decode off the main thread where supported;
    // engines that ignore it simply decode on load as before.
    element.decoding = 'async';
    element.src = src;
  });
}
