/**
 * Handles KLE JSON upload to the Go backend HTTP endpoint.
 * The backend parses KLE, infers scancodes, builds charMap, validates,
 * stores to userdata, and returns a LayoutUploadResponse.
 *
 * Supports two input paths:
 *   - openFilePicker(): native file input (.json)
 *   - uploadFromString(json): raw KLE JSON string (from KLE "Raw data" tab)
 *
 * Both POST to /keyboard/upload. Pass an optional `id` to replace an
 * existing user-uploaded layout instead of creating a new one.
 */

import { useState, useRef, useCallback } from 'react';
import { LayoutUploadResponse } from './types/schema';

export interface KleUploadState {
  result:           LayoutUploadResponse | null;
  isUploading:      boolean;
  error:            string | null;
  openFilePicker:   (replaceId?: string) => void;
  uploadFromString: (json: string, name?: string, replaceId?: string) => Promise<void>;
  clear:            () => void;
}

const UPLOAD_ENDPOINT = '/keyboard/upload';
const MAX_FILE_SIZE   = 512 * 1024;

export function useKleUpload(): KleUploadState {
  const [result,      setResult]      = useState<LayoutUploadResponse | null>(null);
  const [isUploading, setIsUploading] = useState(false);
  const [error,       setError]       = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);

  const uploadRaw = useCallback(async (jsonText: string, name?: string, replaceId?: string) => {
    setIsUploading(true);
    setError(null);
    setResult(null);
    try {
      const params = new URLSearchParams();
      if (name)      params.set('name', name);
      if (replaceId) params.set('id', replaceId);
      const qs = params.toString();
      const url = qs ? `${UPLOAD_ENDPOINT}?${qs}` : UPLOAD_ENDPOINT;

      const response = await fetch(url, {
        method:  'POST',
        headers: { 'Content-Type': 'application/json' },
        body:    jsonText,
      });
      if (!response.ok) {
        const body = await response.json().catch(() => ({ error: response.statusText }));
        throw new Error(body.error ?? `Upload failed: ${response.status}`);
      }
      setResult(await response.json() as LayoutUploadResponse);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setIsUploading(false);
    }
  }, []);

  const ensureInput = useCallback(() => {
    if (inputRef.current) return inputRef.current;
    const input = document.createElement('input');
    input.type   = 'file';
    input.accept = '.json,application/json';
    input.style.display = 'none';
    document.body.appendChild(input);
    inputRef.current = input;
    return input;
  }, []);

  const openFilePicker = useCallback((replaceId?: string) => {
    const input = ensureInput();
    input.onchange = async (e: Event) => {
      const file = (e.target as HTMLInputElement).files?.[0];
      if (!file) return;
      input.value = '';
      if (file.size > MAX_FILE_SIZE) {
        setError(`File too large (${(file.size / 1024).toFixed(0)} KB).`);
        return;
      }
      await uploadRaw(await file.text(), file.name.replace(/\.json$/i, ''), replaceId);
    };
    input.click();
  }, [ensureInput, uploadRaw]);

  const uploadFromString = useCallback(async (json: string, name?: string, replaceId?: string) => {
    await uploadRaw(json, name, replaceId);
  }, [uploadRaw]);

  const clear = useCallback(() => {
    setResult(null); setError(null); setIsUploading(false);
  }, []);

  return { result, isUploading, error, openFilePicker, uploadFromString, clear };
}
