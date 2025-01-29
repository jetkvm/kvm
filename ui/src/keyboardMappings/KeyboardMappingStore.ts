import { getKeyboardMappings, KeyboardLayout } from "@/keyboardMappings/KeyboardLayouts";

// TODO Move this in with all the other stores?

class KeyboardMappingsStore {
  private _layout: KeyboardLayout = KeyboardLayout.US;
  private _subscribers: (() => void)[] = [];

  public keys = getKeyboardMappings(this._layout).keys;
  public chars = getKeyboardMappings(this._layout).chars;
  public modifiers = getKeyboardMappings(this._layout).modifiers;

  setLayout(newLayout: KeyboardLayout) {
    if (this._layout === newLayout) return;
    this._layout = newLayout;
    const updatedMappings = getKeyboardMappings(newLayout);
    this.keys = updatedMappings.keys;
    this.chars = updatedMappings.chars;
    this.modifiers = updatedMappings.modifiers;
    this._notifySubscribers(); 
  }

  getLayout() {
    return this._layout;
  }

  subscribe(callback: () => void) {
    this._subscribers.push(callback);
    return () => {
      this._subscribers = this._subscribers.filter(sub => sub !== callback); // Cleanup
    };
  }

  private _notifySubscribers() {
    this._subscribers.forEach(callback => callback());
  }
}

export const keyboardMappingsStore = new KeyboardMappingsStore();