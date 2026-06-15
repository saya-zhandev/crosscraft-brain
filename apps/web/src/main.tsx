import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import './globals.css';
import { TooltipProvider } from '@/components/ui/tooltip';
import { Toaster } from '@/components/ui/sonner';
import Home from '@/routes/Home';
import EditorRoute from '@/routes/EditorRoute';
import Executions from '@/routes/Executions';
import Credentials from '@/routes/Credentials';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <TooltipProvider delayDuration={300}>
        <Routes>
          <Route path="/" element={<Home />} />
          <Route path="/editor/:id" element={<EditorRoute />} />
          <Route path="/executions/:workflowId" element={<Executions />} />
          <Route path="/credentials" element={<Credentials />} />
        </Routes>
      </TooltipProvider>
      <Toaster />
    </BrowserRouter>
  </React.StrictMode>,
);
