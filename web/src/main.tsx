import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { App } from './App';
import './styles.css';
import './styles/base.css';
import './styles/home.css';
import './styles/media.css';
import './styles/info.css';

const container = document.getElementById('root');
if (container) {
  container.innerHTML = '';
  createRoot(container).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}
