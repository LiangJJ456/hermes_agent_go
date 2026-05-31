import type { ValidationError } from '../model/types';
import { mapError, type ErrorTarget } from '../model/errors';

interface Props {
  errors: ValidationError[];
  onFocus: (target: ErrorTarget) => void;
}

export function ErrorPanel({ errors, onFocus }: Props) {
  if (errors.length === 0) {
    return <div className="errorpanel errorpanel-empty">No errors</div>;
  }
  return (
    <div className="errorpanel">
      <div className="errorpanel-title">Errors ({errors.length})</div>
      <ul>
        {errors.map((e, i) => {
          const m = mapError(e);
          return (
            <li key={i} className="errorrow" onClick={() => onFocus(m.target)}>
              <div className="errorrow-path">{e.path}</div>
              <div className="errorrow-msg">{e.message}</div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
