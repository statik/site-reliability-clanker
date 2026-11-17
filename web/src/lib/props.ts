type Defined<T> = { [K in keyof T]: Exclude<T[K], undefined> };

/**
 * Removes the keys whose value is undefined. Carbon's prop getters return such keys, which
 * `exactOptionalPropertyTypes` does not let a component receive.
 */
export function defined<T extends object>(props: T): Defined<T> {
  const entries = Object.entries(props).filter(([, value]) => value !== undefined);
  return Object.fromEntries(entries) as Defined<T>;
}
